package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"

	"tellurion-risk/internal/breaker"
	"tellurion-risk/internal/event"
	"tellurion-risk/internal/rule"
)

// MaxBodyBytes 是单个请求体上限。管理面只有小 JSON。
const MaxBodyBytes = 1 << 20

// Server 是规则真相源的 HTTP 面（4 个规则管理端点 + 3 个快照端点 + 事件回传 + 熔断指令）。
// monitor/* 三端点因 U7 未裁、形状不冻结，不在本切片（docs/ARCHITECTURE.md §9.4 / §9.6）。
type Server struct {
	svc      *rule.Service
	events   *event.Service
	breakers *breaker.Service
	mux      *http.ServeMux
}

// Option 按需装配可选服务（事件 / 熔断）；不装配则不注册对应路由。
type Option func(*Server)

// WithEvents 装配 X3c 事件回传端点。
func WithEvents(svc *event.Service) Option {
	return func(s *Server) { s.events = svc }
}

// WithBreakers 装配 X3d 熔断指令端点。
func WithBreakers(svc *breaker.Service) Option {
	return func(s *Server) { s.breakers = svc }
}

// New 构造 Server 并注册路由。
func New(svc *rule.Service, opts ...Option) *Server {
	s := &Server{svc: svc, mux: http.NewServeMux()}
	for _, opt := range opts {
		opt(s)
	}
	s.mux.HandleFunc("POST /api/risk/rules", s.createRule)
	s.mux.HandleFunc("GET /api/risk/rules", s.listRules)
	s.mux.HandleFunc("PUT /api/risk/rules/{id}", s.updateRule)
	s.mux.HandleFunc("POST /api/risk/rules/{id}/approve", s.approveRule)
	s.mux.HandleFunc("POST /api/risk/resolve", s.resolve)
	s.mux.HandleFunc("GET /api/risk/snapshots/{hash}", s.getSnapshot)
	// bundle 与快照本体同一形状：规则包就是 canonical snapshot JSON，ParseBundle 直接吃
	//（run 前 1 次，按 hash 缓存永不失效）。
	s.mux.HandleFunc("GET /api/risk/snapshots/{hash}/bundle", s.getSnapshot)
	if s.events != nil {
		s.mux.HandleFunc("POST /api/risk/events", s.ingestEvents)
	}
	if s.breakers != nil {
		s.mux.HandleFunc("POST /api/risk/breakers", s.applyBreaker)
	}
	return s
}

// Handler 返回 HTTP handler。
func (s *Server) Handler() http.Handler { return s.mux }

type errorBody struct {
	Error string `json:"error"`
	Code  string `json:"code"`
}

type ruleBody struct {
	RuleID      string `json:"rule_id"`
	Revision    int    `json:"revision"`
	Status      string `json:"status"`
	VersionHash string `json:"version_hash"`
}

type listBody struct {
	Rules []rule.RuleView `json:"rules"`
	Count int             `json:"count"`
}

type resolveBody struct {
	RiskRulesHash string        `json:"risk_rules_hash"`
	Snapshot      rule.Snapshot `json:"snapshot"`
}

type eventsEnvelope struct {
	Events []json.RawMessage `json:"events"`
}

type eventsResultBody struct {
	Accepted   int             `json:"accepted"`
	Duplicates int             `json:"duplicates"`
	Failed     []event.Failure `json:"failed,omitempty"`
	Code       string          `json:"code,omitempty"`
}

type breakerBody struct {
	BreakerID string `json:"breaker_id"`
	Status    string `json:"status"`
}

func (s *Server) createRule(w http.ResponseWriter, r *http.Request) {
	var req rule.CreateRequest
	if err := decodeBody(w, r, &req); err != nil {
		writeInvalidParams(w, err)
		return
	}
	def, rev, created, err := s.svc.Create(r.Context(), req)
	if err != nil {
		writeError(w, err)
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	writeJSON(w, status, ruleBody{
		RuleID:      def.RuleID,
		Revision:    rev.Revision,
		Status:      string(rev.Status),
		VersionHash: rev.VersionHash,
	})
}

func (s *Server) updateRule(w http.ResponseWriter, r *http.Request) {
	var req rule.UpdateRequest
	if err := decodeBody(w, r, &req); err != nil {
		writeInvalidParams(w, err)
		return
	}
	pathID := r.PathValue("id")
	if req.RuleID == "" {
		req.RuleID = pathID
	}
	if req.RuleID != pathID {
		writeInvalidParams(w, errors.New("rule_id in body does not match path"))
		return
	}
	_, rev, err := s.svc.Update(r.Context(), req)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, ruleBody{
		RuleID:      rev.RuleID,
		Revision:    rev.Revision,
		Status:      string(rev.Status),
		VersionHash: rev.VersionHash,
	})
}

func (s *Server) approveRule(w http.ResponseWriter, r *http.Request) {
	var req rule.ApproveRequest
	if err := decodeBody(w, r, &req); err != nil {
		writeInvalidParams(w, err)
		return
	}
	pathID := r.PathValue("id")
	if req.RuleID == "" {
		req.RuleID = pathID
	}
	if req.RuleID != pathID {
		writeInvalidParams(w, errors.New("rule_id in body does not match path"))
		return
	}
	_, rev, err := s.svc.Approve(r.Context(), req)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, ruleBody{
		RuleID:      rev.RuleID,
		Revision:    rev.Revision,
		Status:      string(rev.Status),
		VersionHash: rev.VersionHash,
	})
}

func (s *Server) listRules(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	filter := rule.Filter{Scope: rule.Scope(q.Get("scope"))}
	switch q.Get("active") {
	case "", "false":
	case "true":
		filter.Active = true
	default:
		writeInvalidParams(w, errors.New("active must be true or false"))
		return
	}
	rules, err := s.svc.List(r.Context(), filter)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, listBody{Rules: rules, Count: len(rules)})
}

// resolve 把 run 前引用解析成快照元数据（钉 hash 或 latest，二选一）。
func (s *Server) resolve(w http.ResponseWriter, r *http.Request) {
	var req rule.ResolveRequest
	if err := decodeBody(w, r, &req); err != nil {
		writeInvalidParams(w, err)
		return
	}
	snap, err := s.svc.Resolve(r.Context(), req)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, resolveBody{RiskRulesHash: snap.RiskRulesHash, Snapshot: snap})
}

func (s *Server) getSnapshot(w http.ResponseWriter, r *http.Request) {
	snap, err := s.svc.GetSnapshot(r.Context(), r.PathValue("hash"))
	if err != nil {
		writeError(w, err)
		return
	}
	// 强幂等：同一 hash 两次拉取字节一致（json.Marshal 对同一结构确定性编码）。
	writeJSON(w, http.StatusOK, snap)
}

// ingestEvents 回传一批裁决事件。整批不可解析 → 400；部分条目失败 → 207 partial_failure；
// 全部成功 → 200。事件回传不在热路径，端点是同步批量确认（调用方只重发 failed）。
func (s *Server) ingestEvents(w http.ResponseWriter, r *http.Request) {
	var req eventsEnvelope
	if err := decodeBody(w, r, &req); err != nil {
		writeInvalidParams(w, err)
		return
	}
	result, err := s.events.Ingest(r.Context(), req.Events)
	if err != nil {
		if errors.Is(err, event.ErrInvalidParams) {
			writeInvalidParams(w, err)
			return
		}
		writeError(w, err)
		return
	}
	status := http.StatusOK
	body := eventsResultBody{Accepted: result.Accepted, Duplicates: result.Duplicates, Failed: result.Failed}
	if len(result.Failed) > 0 {
		status = http.StatusMultiStatus
		body.Code = "partial_failure"
	}
	writeJSON(w, status, body)
}

// applyBreaker 幂等下发一条熔断指令：
// 首次 → 200 {status:"applied"}；同键同载荷重放 → 200 {status:"already_applied"}；
// 同键不同载荷 → 409 idempotency_conflict；未知 action / scope 或字段非法 → 400 invalid_params。
func (s *Server) applyBreaker(w http.ResponseWriter, r *http.Request) {
	var cmd breaker.Command
	if err := decodeBody(w, r, &cmd); err != nil {
		writeInvalidParams(w, err)
		return
	}
	created, err := s.breakers.Apply(r.Context(), cmd)
	if err != nil {
		writeError(w, err)
		return
	}
	status := "already_applied"
	if created {
		status = "applied"
	}
	writeJSON(w, http.StatusOK, breakerBody{BreakerID: cmd.BreakerID, Status: status})
}

// decodeBody 严格解码：拒未知字段、拒尾随内容、限制体积。
func decodeBody(w http.ResponseWriter, r *http.Request, dst any) error {
	if r.Body == nil {
		return errors.New("request body is required")
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, MaxBodyBytes))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		if errors.Is(err, io.EOF) {
			return errors.New("request body is required")
		}
		return err
	}
	if dec.More() {
		return errors.New("request body must contain a single JSON object")
	}
	return nil
}

// classify 把领域哨兵映射成 wire code（词表唯一真相 = docs/api-schema.md §2）。
func classify(err error) (int, string) {
	switch {
	case errors.Is(err, rule.ErrRuleNotFound):
		return http.StatusNotFound, "rule_not_found"
	case errors.Is(err, rule.ErrRevisionNotFound):
		return http.StatusNotFound, "revision_not_found"
	case errors.Is(err, rule.ErrRevisionConflict):
		return http.StatusConflict, "revision_conflict"
	case errors.Is(err, rule.ErrRevisionNotDraft):
		return http.StatusConflict, "revision_not_draft"
	case errors.Is(err, rule.ErrIdempotencyConflict):
		return http.StatusConflict, "idempotency_conflict"
	case errors.Is(err, rule.ErrInvalidRuleType):
		return http.StatusBadRequest, "invalid_rule_type"
	case errors.Is(err, rule.ErrInvalidScope), errors.Is(err, rule.ErrInvalidParams):
		return http.StatusBadRequest, "invalid_params"
	case errors.Is(err, rule.ErrSnapshotNotFound):
		return http.StatusNotFound, "snapshot_not_found"
	case errors.Is(err, rule.ErrNoApprovedRules):
		return http.StatusNotFound, "no_approved_rules"
	case errors.Is(err, rule.ErrSnapshotMismatch):
		// 内容寻址被破坏是服务端事故：500 + 告警（日志），细节不外吐。
		return http.StatusInternalServerError, "internal"
	case errors.Is(err, breaker.ErrIdempotencyConflict):
		return http.StatusConflict, "idempotency_conflict"
	case errors.Is(err, breaker.ErrInvalidParams),
		errors.Is(err, breaker.ErrInvalidAction),
		errors.Is(err, breaker.ErrInvalidScope):
		return http.StatusBadRequest, "invalid_params"
	default:
		return http.StatusInternalServerError, "internal"
	}
}

func writeInvalidParams(w http.ResponseWriter, err error) {
	writeJSON(w, http.StatusBadRequest, errorBody{Error: err.Error(), Code: "invalid_params"})
}

func writeError(w http.ResponseWriter, err error) {
	status, code := classify(err)
	// 4xx 的原文透传给调用方（便于修正请求），5xx 才隐藏细节。
	msg := err.Error()
	if status == http.StatusInternalServerError {
		log.Printf("httpapi: internal error: %v", err)
		msg = "internal error"
	}
	writeJSON(w, status, errorBody{Error: msg, Code: code})
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		log.Printf("httpapi: encode response: %v", err)
	}
}

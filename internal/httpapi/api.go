package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"

	"tellurion-risk/internal/rule"
)

// MaxBodyBytes 是单个请求体上限。管理面只有小 JSON。
const MaxBodyBytes = 1 << 20

// Server 是规则真相源的 HTTP 面（4 个规则管理端点）。
// monitor/* 三端点因 U7 未裁、形状不冻结，不在本切片（docs/ARCHITECTURE.md §9.4 / §9.6）。
type Server struct {
	svc *rule.Service
	mux *http.ServeMux
}

// New 构造 Server 并注册路由。
func New(svc *rule.Service) *Server {
	s := &Server{svc: svc, mux: http.NewServeMux()}
	s.mux.HandleFunc("POST /api/risk/rules", s.createRule)
	s.mux.HandleFunc("GET /api/risk/rules", s.listRules)
	s.mux.HandleFunc("PUT /api/risk/rules/{id}", s.updateRule)
	s.mux.HandleFunc("POST /api/risk/rules/{id}/approve", s.approveRule)
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

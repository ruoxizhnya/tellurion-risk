package httpapi

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"tellurion-risk/internal/event"
)

// monitorExposure 返回窗口内的敞口聚合。只读 risk.risk_events（ADR-0004）。
func (s *Server) monitorExposure(w http.ResponseWriter, r *http.Request) {
	from, to, err := parseWindowParams(r)
	if err != nil {
		writeInvalidParams(w, err)
		return
	}
	q := r.URL.Query()
	report, err := s.events.Exposure(r.Context(), event.ExposureQuery{
		From: from, To: to,
		GroupBy: q.Get("group_by"),
		RunID:   q.Get("run_id"),
		Symbol:  q.Get("symbol"),
		Verdict: q.Get("verdict"),
	})
	if err != nil {
		writeMonitorError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, exposureBody{ExposureReport: report, Configured: s.eventIngestConfigured})
}

// monitorAlerts 返回窗口内的告警列表（verdict=alert 的事件投影）。
func (s *Server) monitorAlerts(w http.ResponseWriter, r *http.Request) {
	from, to, err := parseWindowParams(r)
	if err != nil {
		writeInvalidParams(w, err)
		return
	}
	limit, err := parseLimitParam(r)
	if err != nil {
		writeInvalidParams(w, err)
		return
	}
	q := r.URL.Query()
	report, err := s.events.Alerts(r.Context(), event.AlertsQuery{
		From: from, To: to,
		Severity: q.Get("severity"),
		Rule:     q.Get("rule"),
		RunID:    q.Get("run_id"),
		Symbol:   q.Get("symbol"),
		Limit:    limit,
	})
	if err != nil {
		writeMonitorError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, alertBody{AlertReport: report, Configured: s.eventIngestConfigured})
}

type evaluateRequest struct {
	From        string `json:"from,omitempty"`
	To          string `json:"to,omitempty"`
	RunID       string `json:"run_id,omitempty"`
	Rule        string `json:"rule,omitempty"`
	Symbol      string `json:"symbol,omitempty"`
	MinSeverity string `json:"min_severity,omitempty"`
	Limit       int    `json:"limit,omitempty"`
}

// monitorEvaluate 按给定口径对已回传的告警事件做一次报表评估。
// 不运行检测器、不写库（检测器留 core，ADR-0004）。
func (s *Server) monitorEvaluate(w http.ResponseWriter, r *http.Request) {
	var req evaluateRequest
	if err := decodeBody(w, r, &req); err != nil {
		writeInvalidParams(w, err)
		return
	}
	from, err := parseTimeParam(req.From, "from")
	if err != nil {
		writeInvalidParams(w, err)
		return
	}
	to, err := parseTimeParam(req.To, "to")
	if err != nil {
		writeInvalidParams(w, err)
		return
	}
	report, err := s.events.Evaluate(r.Context(), event.EvaluateQuery{
		From: from, To: to,
		RunID:       req.RunID,
		Rule:        req.Rule,
		Symbol:      req.Symbol,
		MinSeverity: req.MinSeverity,
		Limit:       req.Limit,
	})
	if err != nil {
		writeMonitorError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, evaluationBody{EvaluationReport: report, Configured: s.eventIngestConfigured})
}

// 监控响应 DTO（G4）：在领域报告外**加**一个判别位 `configured`（MINOR，旧读者不感知）。
//
// 空集有两种含义，必须可判别：
//   - `configured:false` ⇒ 本部署**未声明接入**事件回传（core `events_enabled=false`
//     或从未接通）。空集是「未接入」，不是「没触发」。
//   - `configured:true` + 空列表 ⇒ 已声明接入，但窗口内确实没有事件/告警（没触发）。
//
// 本仓看不到 core 的开关，因此该值来自部署声明（`cmd/riskservice` 的
// `RISK_EVENTS_EXPECTED`），不是探测；声明与 core 实际开关不符属于部署错误，
// 核对步骤见 `docs/runbook.md` §5。
type exposureBody struct {
	event.ExposureReport
	Configured bool `json:"configured"`
}

type alertBody struct {
	event.AlertReport
	Configured bool `json:"configured"`
}

type evaluationBody struct {
	event.EvaluationReport
	Configured bool `json:"configured"`
}

func parseWindowParams(r *http.Request) (time.Time, time.Time, error) {
	q := r.URL.Query()
	from, err := parseTimeParam(q.Get("from"), "from")
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	to, err := parseTimeParam(q.Get("to"), "to")
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	return from, to, nil
}

func parseTimeParam(v, name string) (time.Time, error) {
	if v == "" {
		return time.Time{}, nil
	}
	t, err := time.Parse(time.RFC3339, v)
	if err != nil {
		return time.Time{}, fmt.Errorf("%s must be RFC3339: %v", name, err)
	}
	return t, nil
}

func parseLimitParam(r *http.Request) (int, error) {
	v := r.URL.Query().Get("limit")
	if v == "" {
		return 0, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 {
		return 0, errors.New("limit must be a non-negative integer")
	}
	return n, nil
}

// writeMonitorError 把监控领域的失败映射到 wire code：查询口径错误 = 400，
// 读取端口未装配 = 503（拒绝服务，不返回空报告冒充「无敞口 / 无告警」）。
func writeMonitorError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, event.ErrInvalidParams):
		writeInvalidParams(w, err)
	case errors.Is(err, event.ErrMonitorUnavailable):
		writeJSON(w, http.StatusServiceUnavailable, errorBody{
			Error: "monitor reader is not configured",
			Code:  "dependency_unavailable",
		})
	default:
		writeError(w, err)
	}
}

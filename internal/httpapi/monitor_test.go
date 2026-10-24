package httpapi

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"tellurion-risk/internal/breaker"
	"tellurion-risk/internal/event"
	"tellurion-risk/internal/rule"
)

type authFunc func(*http.Request) error

func (f authFunc) Authorize(r *http.Request) error { return f(r) }

func allowAll(*http.Request) error { return nil }

type monitorHarness struct {
	srv    *Server
	events *event.Service
	store  *event.Memory
}

func newMonitorHarness(t *testing.T, opts ...Option) *monitorHarness {
	t.Helper()
	now := func() time.Time { return time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC) }
	ruleSvc, err := rule.New(rule.Options{Store: rule.NewMemory(), Now: now})
	if err != nil {
		t.Fatalf("rule.New: %v", err)
	}
	store := event.NewMemory()
	events, err := event.New(event.Options{Store: store, Now: now})
	if err != nil {
		t.Fatalf("event.New: %v", err)
	}
	breakers, err := breaker.New(breaker.Options{Store: breaker.NewMemory(), Now: now})
	if err != nil {
		t.Fatalf("breaker.New: %v", err)
	}
	base := []Option{WithEvents(events), WithBreakers(breakers)}
	srv := New(ruleSvc, append(base, opts...)...)
	return &monitorHarness{srv: srv, events: events, store: store}
}

func (h *monitorHarness) do(t *testing.T, method, path, body string) (*http.Response, string) {
	t.Helper()
	req := httptest.NewRequest(method, path, bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.srv.Handler().ServeHTTP(rec, req)
	return rec.Result(), rec.Body.String()
}

func (h *monitorHarness) seed(t *testing.T) {
	t.Helper()
	batch := `{"events":[
		{"run_id":"run-1","order_id":"o-buy","event_seq":1,"symbol":"600000.SH","verdict":"allowed","ts":"2026-10-24T11:00:00Z","payload":{"side":"buy","qty":100,"price":10}},
		{"run_id":"run-1","order_id":"o-sell","event_seq":1,"symbol":"600000.SH","verdict":"allowed","ts":"2026-10-24T11:01:00Z","payload":{"side":"sell","qty":40,"price":10}},
		{"run_id":"run-1","order_id":"o-alert","event_seq":1,"symbol":"600000.SH","verdict":"alert","ts":"2026-10-24T11:05:00Z","payload":{"rule":"drawdown","severity":"warning","message":"drawdown 12%"}},
		{"run_id":"run-1","order_id":"o-alert2","event_seq":1,"symbol":"000001.SZ","verdict":"alert","ts":"2026-10-24T11:06:00Z","payload":{"rule":"position_concentration","severity":"critical","message":"position 40%"}},
		{"run_id":"run-1","order_id":"o-bad-alert","event_seq":1,"symbol":"000001.SZ","verdict":"alert","ts":"2026-10-24T11:07:00Z","payload":{"severity":"warning","message":"missing rule"}}
	]}`
	resp, body := h.do(t, http.MethodPost, "/api/risk/events", batch)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("seed events: %d %s", resp.StatusCode, body)
	}
}

func TestMonitor_NoAuthorizerFailsClosed(t *testing.T) {
	h := newMonitorHarness(t) // 不装配 Authorizer
	cases := []struct{ method, path, body string }{
		{http.MethodGet, "/api/risk/monitor/exposure", ""},
		{http.MethodGet, "/api/risk/monitor/alerts", ""},
		{http.MethodPost, "/api/risk/monitor/evaluate", `{}`},
	}
	for _, tc := range cases {
		resp, body := h.do(t, tc.method, tc.path, tc.body)
		wantCode(t, resp.StatusCode, body, http.StatusServiceUnavailable, "auth_unavailable")
	}

	// 闸只挂在 monitor/*：其余端点仍是既有行为（QT-50 ④ 未落），不要顺手扩大/缩小。
	if resp, body := h.do(t, http.MethodGet, "/api/risk/rules", ""); resp.StatusCode != http.StatusOK {
		t.Fatalf("rules endpoint must stay unchanged: %d %s", resp.StatusCode, body)
	}
}

func TestMonitor_AuthorizerOutcomes(t *testing.T) {
	cases := []struct {
		name       string
		auth       authFunc
		wantStatus int
		wantCode   string
	}{
		{"unauthenticated", func(*http.Request) error { return ErrUnauthenticated }, http.StatusUnauthorized, "unauthenticated"},
		{"forbidden", func(*http.Request) error { return ErrForbidden }, http.StatusForbidden, "forbidden"},
		{"broken authorizer", func(*http.Request) error { return errors.New("boom") }, http.StatusInternalServerError, "internal"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newMonitorHarness(t, WithAuthorizer(tc.auth))
			resp, body := h.do(t, http.MethodGet, "/api/risk/monitor/exposure", "")
			wantCode(t, resp.StatusCode, body, tc.wantStatus, tc.wantCode)
		})
	}
}

func TestMonitor_ExposureFromEvents(t *testing.T) {
	h := newMonitorHarness(t, WithAuthorizer(authFunc(allowAll)))
	h.seed(t)

	resp, body := h.do(t, http.MethodGet, "/api/risk/monitor/exposure?group_by=symbol&run_id=run-1", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("exposure: %d %s", resp.StatusCode, body)
	}
	rep := decode[event.ExposureReport](t, body)
	if rep.Source != event.MonitorSource || rep.GroupBy != event.GroupBySymbol {
		t.Fatalf("report header = %+v", rep)
	}
	if rep.Events != 2 || rep.Totals.GrossNotional != 1400 || rep.Totals.NetNotional != 600 {
		t.Fatalf("totals = %+v events=%d", rep.Totals, rep.Events)
	}
	if len(rep.Groups) != 1 || rep.Groups[0].Key != "600000.SH" {
		t.Fatalf("groups = %+v", rep.Groups)
	}

	// 空结果必须是 [] 而不是 null。
	resp, body = h.do(t, http.MethodGet, "/api/risk/monitor/exposure?run_id=nope", "")
	if resp.StatusCode != http.StatusOK || !bytes.Contains([]byte(body), []byte(`"groups":[]`)) {
		t.Fatalf("empty exposure: %d %s", resp.StatusCode, body)
	}

	resp, body = h.do(t, http.MethodGet, "/api/risk/monitor/exposure?group_by=planet", "")
	wantCode(t, resp.StatusCode, body, http.StatusBadRequest, "invalid_params")
	resp, body = h.do(t, http.MethodGet, "/api/risk/monitor/exposure?from=not-a-time", "")
	wantCode(t, resp.StatusCode, body, http.StatusBadRequest, "invalid_params")
}

func TestMonitor_AlertsAndEvaluate(t *testing.T) {
	h := newMonitorHarness(t, WithAuthorizer(authFunc(allowAll)))
	h.seed(t)

	resp, body := h.do(t, http.MethodGet, "/api/risk/monitor/alerts", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("alerts: %d %s", resp.StatusCode, body)
	}
	list := decode[event.AlertReport](t, body)
	if list.Count != 2 || len(list.Alerts) != 2 || list.InvalidEvents != 1 {
		t.Fatalf("alerts = %+v", list)
	}
	if list.Alerts[0].Severity != event.SeverityCritical {
		t.Fatalf("alerts must be newest first: %+v", list.Alerts)
	}

	resp, body = h.do(t, http.MethodGet, "/api/risk/monitor/alerts?severity=bogus", "")
	wantCode(t, resp.StatusCode, body, http.StatusBadRequest, "invalid_params")

	resp, body = h.do(t, http.MethodPost, "/api/risk/monitor/evaluate", `{"min_severity":"warning"}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("evaluate: %d %s", resp.StatusCode, body)
	}
	ev := decode[event.EvaluationReport](t, body)
	if ev.Detection != event.DetectionOutsideProcess || ev.Summary.Total != 2 {
		t.Fatalf("evaluation = %+v", ev)
	}
	if ev.Summary.BySeverity[event.SeverityCritical] != 1 || ev.Summary.ByRule["drawdown"] != 1 {
		t.Fatalf("summary = %+v", ev.Summary)
	}

	// 严格解码 / 参数校验 / 截断 fail-closed。
	for name, tc := range map[string]struct{ body, code string }{
		"unknown field":  {`{"oops":1}`, "invalid_params"},
		"bad from":       {`{"from":"not-a-time"}`, "invalid_params"},
		"bad severity":   {`{"min_severity":"bogus"}`, "invalid_params"},
		"negative limit": {`{"limit":-1}`, "invalid_params"},
		"truncated":      {`{"limit":1}`, "invalid_params"},
		"empty body":     {``, "invalid_params"},
	} {
		t.Run(name, func(t *testing.T) {
			resp, body := h.do(t, http.MethodPost, "/api/risk/monitor/evaluate", tc.body)
			wantCode(t, resp.StatusCode, body, http.StatusBadRequest, tc.code)
		})
	}
}

func TestMonitor_RejectsUnsupportedMethod(t *testing.T) {
	h := newMonitorHarness(t, WithAuthorizer(authFunc(allowAll)))
	resp, _ := h.do(t, http.MethodDelete, "/api/risk/monitor/exposure", "")
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("DELETE monitor/exposure = %d, want 405", resp.StatusCode)
	}
}

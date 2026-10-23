package httpapi

import (
	"net/http"
	"testing"
)

type breakerWire struct {
	BreakerID string `json:"breaker_id"`
	Status    string `json:"status"`
	Code      string `json:"code"`
	Error     string `json:"error"`
}

const breakerJSON = `{"breaker_id":"b-1","action":"halt","scope":"global","reason":"dd breach","ts":"2026-10-24T11:59:00Z"}`

func TestAPI_BreakerIdempotency(t *testing.T) {
	h := newHarness(t)

	resp, body := h.do(t, http.MethodPost, "/api/risk/breakers", breakerJSON)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("first apply: status %d body %s", resp.StatusCode, body)
	}
	first := decode[breakerWire](t, body)
	if first.BreakerID != "b-1" || first.Status != "applied" {
		t.Fatalf("first body: %+v", first)
	}

	// 同键同载荷、重新生成 ts → already_applied（ts 不参与载荷身份）。
	replay := `{"breaker_id":"b-1","action":"halt","scope":"global","reason":"dd breach","ts":"2026-10-24T12:30:00Z"}`
	resp, body = h.do(t, http.MethodPost, "/api/risk/breakers", replay)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("replay: status %d body %s", resp.StatusCode, body)
	}
	if got := decode[breakerWire](t, body); got.Status != "already_applied" {
		t.Fatalf("replay status = %q, want already_applied (body %s)", got.Status, body)
	}

	// 同键不同载荷 → 409 idempotency_conflict。
	diff := `{"breaker_id":"b-1","action":"cancel_all","scope":"global","reason":"dd breach","ts":"2026-10-24T11:59:00Z"}`
	resp, body = h.do(t, http.MethodPost, "/api/risk/breakers", diff)
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("different payload: status %d body %s", resp.StatusCode, body)
	}
	if got := decode[breakerWire](t, body); got.Code != "idempotency_conflict" || got.Error == "" {
		t.Fatalf("different payload body: %+v", got)
	}
}

func TestAPI_BreakerValidation(t *testing.T) {
	h := newHarness(t)
	cases := []string{
		`{"breaker_id":"","action":"halt","scope":"global","ts":"2026-10-24T11:59:00Z"}`,          // 缺 id
		`{"breaker_id":"b-1","action":"liquidate","scope":"global","ts":"2026-10-24T11:59:00Z"}`,  // 未知 action
		`{"breaker_id":"b-1","action":"halt","scope":"planet","ts":"2026-10-24T11:59:00Z"}`,       // 未知 scope
		`{"breaker_id":"b-1","action":"halt","scope":"global"}`,                                   // 缺 ts
		`{"breaker_id":"b-1","action":"halt","scope":"global","ts":"2026-10-24T11:59:00Z","x":1}`, // 未知字段
		``, // 空 body
	}
	for _, body := range cases {
		resp, out := h.do(t, http.MethodPost, "/api/risk/breakers", body)
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("body %q: status %d, want 400 (%s)", body, resp.StatusCode, out)
		}
		if got := decode[breakerWire](t, out); got.Code != "invalid_params" {
			t.Fatalf("body %q: code %q", body, got.Code)
		}
	}
}

func TestAPI_BreakerMethodNotAllowed(t *testing.T) {
	h := newHarness(t)
	if resp, _ := h.do(t, http.MethodGet, "/api/risk/breakers", ""); resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("GET /api/risk/breakers = %d, want 405", resp.StatusCode)
	}
}

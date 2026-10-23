package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"tellurion-risk/internal/breaker"
	"tellurion-risk/internal/event"
	"tellurion-risk/internal/rule"
)

type harness struct {
	srv    *Server
	client *http.Client
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	svc, err := rule.New(rule.Options{
		Store: rule.NewMemory(),
		Now:   func() time.Time { return time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatalf("rule.New: %v", err)
	}
	events, err := event.New(event.Options{
		Store: event.NewMemory(),
		Now:   func() time.Time { return time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatalf("event.New: %v", err)
	}
	breakers, err := breaker.New(breaker.Options{
		Store: breaker.NewMemory(),
		Now:   func() time.Time { return time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatalf("breaker.New: %v", err)
	}
	srv := New(svc, WithEvents(events), WithBreakers(breakers))
	return &harness{srv: srv, client: &http.Client{}}
}

func (h *harness) do(t *testing.T, method, path, body string) (*http.Response, string) {
	t.Helper()
	var rdr *bytes.Reader
	if body == "" {
		rdr = bytes.NewReader(nil)
	} else {
		rdr = bytes.NewReader([]byte(body))
	}
	req := httptest.NewRequest(method, path, rdr)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.srv.Handler().ServeHTTP(rec, req)
	return rec.Result(), rec.Body.String()
}

type wire struct {
	RuleID      string `json:"rule_id"`
	Revision    int    `json:"revision"`
	Status      string `json:"status"`
	VersionHash string `json:"version_hash"`
	Error       string `json:"error"`
	Code        string `json:"code"`
}

type listWire struct {
	Rules []wire `json:"rules"`
	Count int    `json:"count"`
}

func decode[T any](t *testing.T, body string) T {
	t.Helper()
	var out T
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatalf("decode %q: %v", body, err)
	}
	return out
}

func wantCode(t *testing.T, status int, body string, wantStatus int, wantCodeName string) {
	t.Helper()
	if status != wantStatus {
		t.Fatalf("status = %d, want %d (body %s)", status, wantStatus, body)
	}
	got := decode[wire](t, body)
	if got.Code != wantCodeName {
		t.Fatalf("code = %q, want %q (body %s)", got.Code, wantCodeName, body)
	}
	if got.Error == "" {
		t.Fatalf("error message must not be empty (body %s)", body)
	}
}

const createBody = `{"rule_id":"r-sl","scope":"strategy","type":"stop_loss","params":{"pct":2.5},"description":"d"}`

func TestAPI_RuleLifecycle(t *testing.T) {
	h := newHarness(t)

	resp, body := h.do(t, http.MethodPost, "/api/risk/rules", createBody)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create: status %d body %s", resp.StatusCode, body)
	}
	created := decode[wire](t, body)
	if created.RuleID != "r-sl" || created.Revision != 1 || created.Status != "draft" || len(created.VersionHash) != 64 {
		t.Fatalf("create body: %+v", created)
	}

	// 同内容重放 = 200 + 同一 hash。
	resp, body = h.do(t, http.MethodPost, "/api/risk/rules", createBody)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("replay: status %d body %s", resp.StatusCode, body)
	}
	replayed := decode[wire](t, body)
	if replayed.VersionHash != created.VersionHash || replayed.Revision != 1 {
		t.Fatalf("replay must be identical: %+v vs %+v", replayed, created)
	}

	// 列表：head revision 摘要。
	resp, body = h.do(t, http.MethodGet, "/api/risk/rules", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list: status %d body %s", resp.StatusCode, body)
	}
	list := decode[listWire](t, body)
	if list.Count != 1 || len(list.Rules) != 1 || list.Rules[0].VersionHash != created.VersionHash {
		t.Fatalf("list body: %+v", list)
	}

	// 改规则 → revision 2 draft。
	resp, body = h.do(t, http.MethodPut, "/api/risk/rules/r-sl", `{"base_revision":1,"params":{"pct":3}}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("update: status %d body %s", resp.StatusCode, body)
	}
	updated := decode[wire](t, body)
	if updated.Revision != 2 || updated.Status != "draft" || updated.VersionHash == created.VersionHash {
		t.Fatalf("update body: %+v", updated)
	}

	// 审批激活。
	resp, body = h.do(t, http.MethodPost, "/api/risk/rules/r-sl/approve", `{"revision":2}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("approve: status %d body %s", resp.StatusCode, body)
	}
	approved := decode[wire](t, body)
	if approved.Revision != 2 || approved.Status != "active" || approved.VersionHash != updated.VersionHash {
		t.Fatalf("approve body: %+v", approved)
	}

	// 审批重放幂等。
	resp, body = h.do(t, http.MethodPost, "/api/risk/rules/r-sl/approve", `{"revision":2}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("approve replay: status %d body %s", resp.StatusCode, body)
	}
	if decode[wire](t, body).VersionHash != approved.VersionHash {
		t.Fatalf("approve replay must keep hash: %s", body)
	}

	// active=true 命中；scope 过滤命中。
	resp, body = h.do(t, http.MethodGet, "/api/risk/rules?active=true&scope=strategy", "")
	if resp.StatusCode != http.StatusOK || decode[listWire](t, body).Count != 1 {
		t.Fatalf("active list: status %d body %s", resp.StatusCode, body)
	}
	resp, body = h.do(t, http.MethodGet, "/api/risk/rules?scope=global", "")
	if resp.StatusCode != http.StatusOK || decode[listWire](t, body).Count != 0 {
		t.Fatalf("scope list must drop others: status %d body %s", resp.StatusCode, body)
	}
}

func TestAPI_EmptyListIsArrayNotNull(t *testing.T) {
	h := newHarness(t)
	resp, body := h.do(t, http.MethodGet, "/api/risk/rules", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
	if !bytes.Contains([]byte(body), []byte(`"rules":[]`)) {
		t.Fatalf("empty list must encode as [], got %s", body)
	}
	if decode[listWire](t, body).Count != 0 {
		t.Fatalf("count must be 0: %s", body)
	}
}

func TestAPI_WireCodes(t *testing.T) {
	setup := func(t *testing.T) *harness {
		h := newHarness(t)
		if resp, body := h.do(t, http.MethodPost, "/api/risk/rules", createBody); resp.StatusCode != http.StatusCreated {
			t.Fatalf("setup create: %d %s", resp.StatusCode, body)
		}
		// 让 r-sl 有一个已激活的 v1 与一个 draft v2（用于废弃后审批的用例）。
		if resp, body := h.do(t, http.MethodPost, "/api/risk/rules/r-sl/approve", `{"revision":1}`); resp.StatusCode != http.StatusOK {
			t.Fatalf("setup approve: %d %s", resp.StatusCode, body)
		}
		if resp, body := h.do(t, http.MethodPut, "/api/risk/rules/r-sl", `{"base_revision":1,"params":{"pct":4}}`); resp.StatusCode != http.StatusOK {
			t.Fatalf("setup update: %d %s", resp.StatusCode, body)
		}
		if resp, body := h.do(t, http.MethodPost, "/api/risk/rules/r-sl/approve", `{"revision":2}`); resp.StatusCode != http.StatusOK {
			t.Fatalf("setup approve v2: %d %s", resp.StatusCode, body)
		}
		return h
	}

	cases := []struct {
		name       string
		method     string
		path       string
		body       string
		wantStatus int
		wantCode   string
	}{
		{"unknown type", http.MethodPost, "/api/risk/rules",
			`{"rule_id":"x","scope":"global","type":"magic","params":{}}`, 400, "invalid_rule_type"},
		{"unknown scope", http.MethodPost, "/api/risk/rules",
			`{"rule_id":"x","scope":"planet","type":"regime","params":{}}`, 400, "invalid_params"},
		{"params missing", http.MethodPost, "/api/risk/rules",
			`{"rule_id":"x","scope":"global","type":"regime"}`, 400, "invalid_params"},
		{"params not object", http.MethodPost, "/api/risk/rules",
			`{"rule_id":"x","scope":"global","type":"regime","params":[1]}`, 400, "invalid_params"},
		{"unknown field", http.MethodPost, "/api/risk/rules",
			`{"rule_id":"x","scope":"global","type":"regime","params":{},"oops":1}`, 400, "invalid_params"},
		{"trailing object", http.MethodPost, "/api/risk/rules",
			`{"rule_id":"x","scope":"global","type":"regime","params":{}}{"a":1}`, 400, "invalid_params"},
		{"empty body", http.MethodPost, "/api/risk/rules", "", 400, "invalid_params"},
		{"same id different content", http.MethodPost, "/api/risk/rules",
			`{"rule_id":"r-sl","scope":"strategy","type":"stop_loss","params":{"pct":9}}`, 409, "idempotency_conflict"},
		{"update unknown rule", http.MethodPut, "/api/risk/rules/nope",
			`{"base_revision":1,"params":{}}`, 404, "rule_not_found"},
		{"update stale base", http.MethodPut, "/api/risk/rules/r-sl",
			`{"base_revision":1,"params":{"pct":5}}`, 409, "revision_conflict"},
		{"update body id mismatch", http.MethodPut, "/api/risk/rules/r-sl",
			`{"rule_id":"other","base_revision":2,"params":{"pct":5}}`, 400, "invalid_params"},
		{"approve unknown rule", http.MethodPost, "/api/risk/rules/nope/approve", `{"revision":1}`, 404, "rule_not_found"},
		{"approve unknown revision", http.MethodPost, "/api/risk/rules/r-sl/approve", `{"revision":99}`, 404, "revision_not_found"},
		{"approve deprecated", http.MethodPost, "/api/risk/rules/r-sl/approve", `{"revision":1}`, 409, "revision_not_draft"},
		{"approve body id mismatch", http.MethodPost, "/api/risk/rules/r-sl/approve",
			`{"rule_id":"other","revision":2}`, 400, "invalid_params"},
		{"bad active flag", http.MethodGet, "/api/risk/rules?active=maybe", "", 400, "invalid_params"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := setup(t)
			resp, body := h.do(t, tc.method, tc.path, tc.body)
			wantCode(t, resp.StatusCode, body, tc.wantStatus, tc.wantCode)
		})
	}
}

func TestAPI_RejectsUnsupportedMethod(t *testing.T) {
	h := newHarness(t)
	resp, _ := h.do(t, http.MethodDelete, "/api/risk/rules", "")
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("DELETE /api/risk/rules = %d, want 405", resp.StatusCode)
	}
}

func TestAPI_ApproveAndListAcrossRules(t *testing.T) {
	h := newHarness(t)
	other := `{"rule_id":"r-regime","scope":"global","type":"regime","params":{"window":20}}`
	if resp, body := h.do(t, http.MethodPost, "/api/risk/rules", other); resp.StatusCode != http.StatusCreated {
		t.Fatalf("create other: %d %s", resp.StatusCode, body)
	}
	if resp, body := h.do(t, http.MethodPost, "/api/risk/rules", createBody); resp.StatusCode != http.StatusCreated {
		t.Fatalf("create base: %d %s", resp.StatusCode, body)
	}

	resp, body := h.do(t, http.MethodGet, "/api/risk/rules?active=false", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list: %d %s", resp.StatusCode, body)
	}
	list := decode[listWire](t, body)
	if list.Count != 2 || list.Rules[0].RuleID != "r-regime" || list.Rules[1].RuleID != "r-sl" {
		t.Fatalf("list must be stable and sorted: %+v", list)
	}
}

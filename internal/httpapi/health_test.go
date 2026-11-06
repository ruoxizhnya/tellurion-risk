package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"tellurion-risk/internal/breaker"
	"tellurion-risk/internal/rule"
)

func newTestService(t *testing.T) *rule.Service {
	t.Helper()
	svc, err := rule.New(rule.Options{Store: rule.NewMemory()})
	if err != nil {
		t.Fatalf("rule.New: %v", err)
	}
	return svc
}

func probe(t *testing.T, h http.Handler, method, path string) (int, string) {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(method, path, nil))
	return rec.Code, rec.Body.String()
}

// G1 ⑤①：/healthz 无需 token；/readyz 在依赖不可用 503、恢复后 200。
func TestProbes_ArePublicAndReadinessFlips(t *testing.T) {
	svc := newTestService(t)

	readyErr := errors.New("postgres unreachable")
	h := New(svc, WithReadiness(func(context.Context) error { return readyErr })).Handler()

	if code, body := probe(t, h, http.MethodGet, "/healthz"); code != http.StatusOK || !strings.Contains(body, `"ok"`) {
		t.Fatalf("healthz = %d %s, want 200 ok", code, body)
	}
	if code, body := probe(t, h, http.MethodGet, "/readyz"); code != http.StatusServiceUnavailable ||
		!strings.Contains(body, "not_ready") {
		t.Fatalf("readyz(down) = %d %s, want 503 not_ready", code, body)
	}

	readyErr = nil
	if code, body := probe(t, h, http.MethodGet, "/readyz"); code != http.StatusOK || !strings.Contains(body, "ready") {
		t.Fatalf("readyz(up) = %d %s, want 200 ready", code, body)
	}

	// 内存档不注入 readiness：就绪 = 进程活着，并显式标注 store=memory。
	mem := New(svc).Handler()
	if code, body := probe(t, mem, http.MethodGet, "/readyz"); code != http.StatusOK ||
		!strings.Contains(body, `"memory"`) {
		t.Fatalf("readyz(memory) = %d %s, want 200 store=memory", code, body)
	}
}

// G1 ⑤⑤：限流 429 与鉴权 401 可区分，且探针豁免。
func TestRateLimitDistinctFromAuthAndProbesExempt(t *testing.T) {
	svc := newTestService(t)
	allow := authFunc(func(*http.Request) (Principal, error) {
		return Principal{Username: "alice", Role: "viewer"}, nil
	})
	h := New(svc, WithAuthorizer(allow), WithRateLimit(1, 1)).Handler()

	if code, body := probe(t, h, http.MethodGet, "/api/risk/rules"); code != http.StatusOK {
		t.Fatalf("first request = %d %s, want 200", code, body)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/risk/rules", nil))
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("second request = %d %s, want 429", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "rate_limited") {
		t.Fatalf("429 body = %s, want code rate_limited", rec.Body.String())
	}
	if rec.Header().Get("Retry-After") == "" {
		t.Fatal("429 must carry Retry-After")
	}

	// 鉴权失败先于限流：连续多次仍是 401，不会因为「第一个桶空了」被读成 429。
	deny := authFunc(func(*http.Request) (Principal, error) { return Principal{}, ErrUnauthenticated })
	denied := New(svc, WithAuthorizer(deny), WithRateLimit(1, 1)).Handler()
	for i := 0; i < 3; i++ {
		code, body := probe(t, denied, http.MethodGet, "/api/risk/rules")
		if code != http.StatusUnauthorized || !strings.Contains(body, "unauthenticated") {
			t.Fatalf("attempt %d = %d %s, want 401 unauthenticated", i, code, body)
		}
	}

	// 探针不经过限流：桶已经空了，连续 5 次仍然 200。
	for i := 0; i < 5; i++ {
		if code, body := probe(t, h, http.MethodGet, "/healthz"); code != http.StatusOK {
			t.Fatalf("healthz attempt %d = %d %s, want 200", i, code, body)
		}
	}
}

// G1 ⑤③：错误码闭合表抽 3 条对拍实现。
func TestClassifyMatchesDocumentedTable(t *testing.T) {
	cases := []struct {
		err    error
		status int
		code   string
	}{
		{rule.ErrRuleNotFound, http.StatusNotFound, "rule_not_found"},
		{breaker.ErrIdempotencyConflict, http.StatusConflict, "idempotency_conflict"},
		{rule.ErrRevisionConflict, http.StatusConflict, "revision_conflict"},
	}
	for _, tc := range cases {
		status, code := classify(errors.Join(errors.New("wrapped"), tc.err))
		if status != tc.status || code != tc.code {
			t.Fatalf("%v => %d/%s, want %d/%s", tc.err, status, code, tc.status, tc.code)
		}
	}
}

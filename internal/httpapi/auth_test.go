package httpapi

import (
	"net/http"
	"sort"
	"testing"

	"github.com/ruoxizhnya/tellurion-kit/auth"

	"tellurion-risk/internal/rule"
)

// TestRouteTableEqualsPolicyTable 是本仓的鉴权守卫：注册的路由与策略表必须
// 双向一一对应。漏挂策略（新端点写进来但没决定谁能用）或表里写了不存在的
// 路由（端点了 / 改名了）都会红。
func TestRouteTableEqualsPolicyTable(t *testing.T) {
	h := newHarness(t) // 全量装配：事件 + 熔断都开，路由表是完整面
	routes := h.srv.routePatterns()
	policy := PolicyEntries()

	if len(routes) != len(policy) {
		t.Fatalf("route table has %d entries, policy has %d: routes=%v policy=%v",
			len(routes), len(policy), routes, sortedKeys(policy))
	}
	for _, r := range routes {
		if _, ok := policy[r]; !ok {
			t.Errorf("route %q has no policy entry (undeclared endpoints must not ship)", r)
		}
	}
	seen := make(map[string]bool, len(routes))
	for _, r := range routes {
		if seen[r] {
			t.Errorf("route %q registered twice", r)
		}
		seen[r] = true
	}
	for k := range policy {
		if !seen[k] {
			t.Errorf("policy entry %q has no registered route", k)
		}
	}
}

func sortedKeys[T any](m map[string]T) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func issueToken(t *testing.T, svc *auth.TokenService, username, role string) string {
	t.Helper()
	tok, _, err := svc.IssueTokens(1, username, role)
	if err != nil {
		t.Fatalf("issue token: %v", err)
	}
	return tok
}

// TestAuth_ThreeStatesAndActor 用真 kit TokenService + 真策略表跑 wire：
// 未装配闸 503、无凭证 401、越权 403、合法角色 2xx，审计 actor 取自 claims。
func TestAuth_ThreeStatesAndActor(t *testing.T) {
	svc := auth.NewTokenService(auth.Config{JWTSecret: []byte("risk-auth-test")})
	if !svc.Enabled() {
		t.Fatal("token service must be enabled")
	}
	h := newHarnessWithAuthorizer(t, NewAuthorizer(svc))

	viewer := issueToken(t, svc, "alice", "viewer")
	trader := issueToken(t, svc, "bob", "trader")
	admin := issueToken(t, svc, "carol", "admin")
	_, refresh, err := svc.IssueTokens(2, "bob", "trader")
	if err != nil {
		t.Fatalf("issue refresh: %v", err)
	}

	// 无凭证 / 坏 token / refresh 当 access ⇒ 401。
	for name, token := range map[string]string{
		"no token":      "",
		"malformed":     "not-a-jwt",
		"refresh token": refresh,
	} {
		resp, body := h.doAuth(t, http.MethodGet, "/api/risk/rules", "", token)
		wantCode(t, resp.StatusCode, body, http.StatusUnauthorized, "unauthenticated")
		_ = name
	}

	// 读端点 viewer，写端点 trader，审批 admin。
	resp, body := h.doAuth(t, http.MethodGet, "/api/risk/rules", "", viewer)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("viewer GET rules: %d %s", resp.StatusCode, body)
	}
	resp, body = h.doAuth(t, http.MethodPost, "/api/risk/rules", createBody, viewer)
	wantCode(t, resp.StatusCode, body, http.StatusForbidden, "forbidden")

	resp, body = h.doAuth(t, http.MethodPost, "/api/risk/rules", createBody, trader)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("trader create: %d %s", resp.StatusCode, body)
	}
	resp, body = h.doAuth(t, http.MethodPost, "/api/risk/rules/r-sl/approve", `{"revision":1}`, trader)
	wantCode(t, resp.StatusCode, body, http.StatusForbidden, "forbidden")
	resp, body = h.doAuth(t, http.MethodPost, "/api/risk/rules/r-sl/approve", `{"revision":1}`, admin)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("admin approve: %d %s", resp.StatusCode, body)
	}

	// 审计 actor 取自 claims（bob 建、carol 批），任何一条都不许是空串。
	resp, body = h.doAuth(t, http.MethodGet, "/api/risk/audit?rule_id=r-sl", "", viewer)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("viewer audit: %d %s", resp.StatusCode, body)
	}
	audit := decode[struct {
		Audit []rule.AuditEntry `json:"audit"`
		Count int               `json:"count"`
	}](t, body)
	if audit.Count != 2 || len(audit.Audit) != 2 {
		t.Fatalf("audit = %+v", audit)
	}
	for _, entry := range audit.Audit {
		if entry.Actor == "" {
			t.Fatalf("audit actor must not be empty: %+v", entry)
		}
	}
	if audit.Audit[0].Action != rule.ActionCreate || audit.Audit[0].Actor != "bob" {
		t.Fatalf("create audit = %+v", audit.Audit[0])
	}
	if audit.Audit[1].Action != rule.ActionApprove || audit.Audit[1].Actor != "carol" {
		t.Fatalf("approve audit = %+v", audit.Audit[1])
	}

	// 审计读缺 rule_id ⇒ 400（不是静默返回全表）。
	resp, body = h.doAuth(t, http.MethodGet, "/api/risk/audit", "", viewer)
	wantCode(t, resp.StatusCode, body, http.StatusBadRequest, "invalid_params")

	// 闸未装配 ⇒ 所有端点 503，不是放行。
	noGate := newHarnessWithAuthorizer(t, nil)
	resp, body = noGate.do(t, http.MethodGet, "/api/risk/rules", "")
	wantCode(t, resp.StatusCode, body, http.StatusServiceUnavailable, "auth_unavailable")
}

// TestAuth_OpenAccessOnLoopback 锁死显式关闭鉴权的语义：无 secret + 回环时
// TokenService 未启用，所有端点放行，审计 actor 用显式常量（仍不为空）。
func TestAuth_OpenAccessOnLoopback(t *testing.T) {
	disabled := auth.NewTokenService(auth.Config{})
	if disabled.Enabled() {
		t.Fatal("expected disabled token service")
	}
	h := newHarnessWithAuthorizer(t, NewAuthorizer(disabled))

	resp, body := h.doAuth(t, http.MethodPost, "/api/risk/rules", createBody, "")
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("open access create: %d %s", resp.StatusCode, body)
	}
	resp, body = h.doAuth(t, http.MethodGet, "/api/risk/audit?rule_id=r-sl", "", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("open access audit: %d %s", resp.StatusCode, body)
	}
	audit := decode[struct {
		Audit []rule.AuditEntry `json:"audit"`
	}](t, body)
	if len(audit.Audit) != 1 || audit.Audit[0].Actor != ActorOpenAccess {
		t.Fatalf("open-access actor must be %q, got %+v", ActorOpenAccess, audit.Audit)
	}
}

package httpapi

import (
	"net/http"
	"testing"

	"github.com/ruoxizhnya/tellurion-kit/auth"
)

// serviceRole 是裁-17 定的服务间角色名（core 代机器调用本仓时用）。
const serviceRole = "service"

// TestPolicy_ServiceRoleScope 把「service 只开两条服务间链路」钉死：
// `POST /api/risk/resolve` 与 `POST /api/risk/events` 并列开 service；
// 审计读与规则管理面（rules / approve）**不开**——那些是人负责的动作，
// 不该由机器凭证代劳。新增档位**不放松**既有语义：viewer 调 resolve 仍 403。
//
// 角色 any-of 且 kit 不做角色排序 ⇒ service 必须与 trader / admin 并列写在同一条
// Roles 里，缺一个就是「另一类调用方被拒」，所以这里逐条断言整份角色集合。
//
// ⚠️ **token 形状的已知跨仓缺口（不在本单）**：本用例用 kit `IssueTokens` 签的数字 subject
// token（`subject="1"`），因为 kit `auth/httpadapter.Authenticate` 要求 subject 可解析为
// `int64`；而裁-17 的生产口径 `sub=service:<repo>`（kit `v0.1.2` 的 `IssueServiceToken`）
// 是**非数字** subject ⇒ 会被 kit 的守卫判 401「malformed token subject」，压根到不了本仓的
// 角色判定（实测见 `docs/TASKS.md` QT-42 施工记录「跨仓缺口」条）。
// ⇒ 本用例证明的是**本仓策略表**（角色档位与作用域）正确，**不**证明生产 token 能走通；
// 那条缺口归 kit（放宽 subject 校验）或改裁-17 的 subject 口径，**已报协调者**。
func TestPolicy_ServiceRoleScope(t *testing.T) {
	svc := auth.NewTokenService(auth.Config{JWTSecret: []byte("risk-service-role-test")})
	h := newHarnessWithAuthorizer(t, NewAuthorizer(svc))

	viewer := issueToken(t, svc, "alice", "viewer")
	trader := issueToken(t, svc, "bob", "trader")
	admin := issueToken(t, svc, "carol", "admin")
	// username 记成 service:core（审计 actor 语义），subject 仍是 kit 要求的数字。
	service := issueToken(t, svc, "service:core", serviceRole)

	// 先用真人身份建一条已审批规则，让 resolve 有东西可解析（无生效规则 ⇒ 404）。
	resp, body := h.doAuth(t, http.MethodPost, "/api/risk/rules", createBody, trader)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("setup create: %d %s", resp.StatusCode, body)
	}
	resp, body = h.doAuth(t, http.MethodPost, "/api/risk/rules/r-sl/approve", `{"revision":1}`, admin)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("setup approve: %d %s", resp.StatusCode, body)
	}

	// service 调 resolve ⇒ 200（服务间链路之一）。
	resp, body = h.doAuth(t, http.MethodPost, "/api/risk/resolve", `{"ruleset":"latest"}`, service)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("service resolve: %d %s", resp.StatusCode, body)
	}

	// service 调 events ⇒ 200（服务间链路之二）。
	resp, body = h.doAuth(t, http.MethodPost, "/api/risk/events",
		`{"events":[{"run_id":"run-1","order_id":"ord-1","event_seq":1,"verdict":"allowed","ts":"2026-10-24T11:59:00Z"}]}`, service)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("service events: %d %s", resp.StatusCode, body)
	}

	// service 调 audit ⇒ 403（审计是人操作留痕，机器凭证不读）。
	resp, body = h.doAuth(t, http.MethodGet, "/api/risk/audit?rule_id=r-sl", "", service)
	wantCode(t, resp.StatusCode, body, http.StatusForbidden, "forbidden")

	// service 调规则管理面 ⇒ 403（建规则 / 审批都不开）。
	resp, body = h.doAuth(t, http.MethodPost, "/api/risk/rules", createBody, service)
	wantCode(t, resp.StatusCode, body, http.StatusForbidden, "forbidden")
	resp, body = h.doAuth(t, http.MethodPost, "/api/risk/rules/r-sl/approve", `{"revision":1}`, service)
	wantCode(t, resp.StatusCode, body, http.StatusForbidden, "forbidden")

	// 新增档位不放松既有语义：viewer 调 resolve 仍 403（resolve 是写口径）。
	resp, body = h.doAuth(t, http.MethodPost, "/api/risk/resolve", `{"ruleset":"latest"}`, viewer)
	wantCode(t, resp.StatusCode, body, http.StatusForbidden, "forbidden")
}

// TestPolicy_ServiceRoleDeclaredExactlyTwice 钉住 service 的**作用域**：
// 只在 resolve 与 events 两条端点上声明，别处多一条就是权限面被悄悄放大
// （禁区：audit / rules / approve 一律不开 service）。
func TestPolicy_ServiceRoleDeclaredExactlyTwice(t *testing.T) {
	allowed := map[string]bool{
		"POST /api/risk/resolve": true,
		"POST /api/risk/events":  true,
	}
	declared := 0
	for route, rule := range PolicyEntries() {
		has := false
		for _, role := range rule.Roles {
			if role == serviceRole {
				has = true
				break
			}
		}
		if has && !allowed[route] {
			t.Errorf("route %q must not declare role %q", route, serviceRole)
		}
		if !has && allowed[route] {
			t.Errorf("route %q must declare role %q", route, serviceRole)
		}
		if has {
			declared++
		}
	}
	// 条数也要对：少一条 = 某条服务间链路被悄悄关掉（表项被删时循环看不到它）。
	if declared != len(allowed) {
		t.Errorf("role %q declared on %d routes, want %d", serviceRole, declared, len(allowed))
	}
}

// TestPolicy_ServiceRoleCoexistsWithHumanRoles 钉住「any-of 且并列」的写法：
// 两条服务间端点上，service 必须与真人角色（trader / admin）**同在一条 Roles**。
// 角色列表不做排序，所以「service 顶替 trader/admin」会让真人调用方直接 403。
func TestPolicy_ServiceRoleCoexistsWithHumanRoles(t *testing.T) {
	for _, route := range []string{"POST /api/risk/resolve", "POST /api/risk/events"} {
		rule, ok := PolicyEntries()[route]
		if !ok {
			t.Fatalf("route %q has no policy entry", route)
		}
		have := map[string]bool{}
		for _, role := range rule.Roles {
			have[role] = true
		}
		for _, want := range []string{serviceRole, "trader", "admin"} {
			if !have[want] {
				t.Errorf("route %q roles %v must include %q", route, rule.Roles, want)
			}
		}
	}
}

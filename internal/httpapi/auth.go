package httpapi

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"

	"github.com/ruoxizhnya/tellurion-kit/auth"
	kithttp "github.com/ruoxizhnya/tellurion-kit/auth/httpadapter"
)

// 鉴权闸的失败分类。Authorizer 的实现（kit auth/httpadapter 适配）只负责
// 把凭证 / 角色判定映射到这两个哨兵；本仓不另定鉴权契约（ARCHITECTURE.md §9.4）。
var (
	ErrUnauthenticated = errors.New("no valid credentials")
	ErrForbidden       = errors.New("insufficient role")
)

// Principal 是鉴权通过后的调用方身份。Audit 的 actor 从这里取，不由请求体自报。
type Principal struct {
	Username string
	Subject  string
	Role     string
}

// ActorOpenAccess 是鉴权显式关闭（无 secret + 回环）时的审计 actor。
// 审计 actor 不允许为空串：空串无法区分「未认证」与「忘了填」。
const ActorOpenAccess = "open-access"

type principalKey struct{}

// Authorizer 是本仓所有端点的鉴权闸。nil = 闸未装配，端点必须拒绝服务。
type Authorizer interface {
	Authorize(r *http.Request) (Principal, error)
}

// WithAuthorizer 装配端点鉴权闸。
func WithAuthorizer(a Authorizer) Option {
	return func(s *Server) { s.authorizer = a }
}

// NewAuthorizer 用 kit 的 net/http 适配构造鉴权闸：token 解析 / kind=access /
// 角色 any-of 都在 kit，本仓只提供 policy 表（policy.go）。
//
// 鉴权显式关闭（TokenService 未启用，仅限无 secret + 回环）时返回零身份 + nil，
// 与 kit Guard 的 open-access 语义一致；审计 actor 由 ActorOpenAccess 兜底。
func NewAuthorizer(svc *auth.TokenService) Authorizer {
	return kitAuthorizer{svc: svc}
}

type kitAuthorizer struct {
	svc *auth.TokenService
}

func (a kitAuthorizer) Authorize(r *http.Request) (Principal, error) {
	if a.svc == nil || !a.svc.Enabled() {
		return Principal{}, nil
	}
	key := kithttp.RouteKey(r)
	rule, declared := riskPolicy.Rule(key)
	if !declared {
		// 路由表 ≡ policy 表由守卫测试钉住；运行时仍 fail-closed，绝不因漏登而放行。
		return Principal{}, fmt.Errorf("%w: no authorization policy for %q", ErrForbidden, key)
	}
	claims, err := kithttp.Authenticate(a.svc, r)
	if err != nil {
		return Principal{}, fmt.Errorf("%w: %v", ErrUnauthenticated, err)
	}
	if err := kithttp.Authorize(claims, rule); err != nil {
		if errors.Is(err, kithttp.ErrForbidden) {
			return Principal{}, fmt.Errorf("%w: %v", ErrForbidden, err)
		}
		return Principal{}, fmt.Errorf("%w: %v", ErrUnauthenticated, err)
	}
	return Principal{Username: claims.Username, Subject: claims.Subject, Role: claims.Role}, nil
}

// requireAuth 是 fail-closed 闸：未装配 Authorizer 时返回 503（拒绝而非放行），
// 凭证缺失 401、角色不足 403；只有 Authorize 返回 nil 才进入 handler。
func (s *Server) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.authorizer == nil {
			writeJSON(w, http.StatusServiceUnavailable, errorBody{
				Error: "endpoints require an authorizer; none is configured",
				Code:  "auth_unavailable",
			})
			return
		}
		principal, err := s.authorizer.Authorize(r)
		switch {
		case err == nil:
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), principalKey{}, principal)))
		case errors.Is(err, ErrUnauthenticated):
			writeJSON(w, http.StatusUnauthorized, errorBody{Error: "authentication required", Code: "unauthenticated"})
		case errors.Is(err, ErrForbidden):
			writeJSON(w, http.StatusForbidden, errorBody{Error: "insufficient role", Code: "forbidden"})
		default:
			log.Printf("httpapi: authorizer error: %v", err)
			writeJSON(w, http.StatusInternalServerError, errorBody{Error: "internal error", Code: "internal"})
		}
	})
}

// principalFrom 取 requireAuth 放进 context 的身份。
func principalFrom(ctx context.Context) (Principal, bool) {
	p, ok := ctx.Value(principalKey{}).(Principal)
	return p, ok
}

// actorOf 返回审计 actor：已认证取 claims 的用户名（缺失时退 subject），
// open-access 取显式常量。永远不返回空串。
func actorOf(r *http.Request) string {
	if p, ok := principalFrom(r.Context()); ok {
		if p.Username != "" {
			return p.Username
		}
		if p.Subject != "" {
			return p.Subject
		}
	}
	return ActorOpenAccess
}

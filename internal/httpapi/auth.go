package httpapi

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/ruoxizhnya/tellurion-kit/auth"
	kithttp "github.com/ruoxizhnya/tellurion-kit/auth/httpadapter"
)

// 鉴权闸的失败分类。Authorizer 的实现（kit auth/httpadapter 适配）只负责
// 把凭证 / 角色判定映射到这三个哨兵；本仓不另定鉴权契约（ARCHITECTURE.md §9.4）。
var (
	ErrUnauthenticated = errors.New("no valid credentials")
	ErrForbidden       = errors.New("insufficient role")
	// ErrAuthUnavailable 表示鉴权闸存在但鉴权**未配置**（无 JWT secret）。
	// 第 2 态的正确语义：写端点 503 拒绝（fail-closed），读端点保持公开；
	// 绝不能把「没配 secret」当成「已通过」。
	ErrAuthUnavailable = errors.New("authentication is not configured")
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
// 三种启动态（六仓统一口径）：
//  1. TokenService 已启用（有 JWT secret）⇒ 按 policy 表全闸；
//  2. 未启用且**未显式声明开放**（无 secret + 回环）⇒ 读端点公开，写端点
//     一律 ErrAuthUnavailable（requireAuth 映射 503 auth_unavailable）；
//  3. 未启用且显式声明 AUTH_INSECURE_EXPOSURE=loopback-published ⇒ 装配
//     NewOpenAccessAuthorizer，写端点放行并由启动 WARN 点名。
//
// 第 2 态的存在是为了让「忘了配 secret」不再等于「所有写端点都敞开」。
func NewAuthorizer(svc *auth.TokenService) Authorizer {
	return kitAuthorizer{svc: svc}
}

// NewOpenAccessAuthorizer 是第 3 态（无 secret + 显式 loopback-published）的闸：
// 全部端点放行，审计 actor 由 ActorOpenAccess 兜底。只有启动闸确认了显式声明
// 才允许装配它 —— 不得用它来表达「忘了配 secret」。
func NewOpenAccessAuthorizer() Authorizer { return openAccessAuthorizer{} }

type openAccessAuthorizer struct{}

func (openAccessAuthorizer) Authorize(*http.Request) (Principal, error) {
	return Principal{}, nil
}

type kitAuthorizer struct {
	svc *auth.TokenService
}

func (a kitAuthorizer) Authorize(r *http.Request) (Principal, error) {
	if a.svc == nil || !a.svc.Enabled() {
		// 第 2 态：没配 secret 不是「已通过」。读端点保持公开（与 G1 的公开
		// 探针同口径），写端点 fail-closed。
		if isWriteRequest(r) {
			return Principal{}, fmt.Errorf("%w: JWT secret is not configured", ErrAuthUnavailable)
		}
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

// isWriteRequest 判定「写口径」端点，供第 2 态决定是否 fail-closed。
//
// 默认按 HTTP 方法判定（非读方法一律算写，未知新路由 fail-closed）；
// POST /api/risk/monitor/evaluate 是唯一例外：它只做报表评估、不写库
// （monitor.go 的 evaluate 路径零写），因此按读口径保持公开。
func isWriteRequest(r *http.Request) bool {
	switch r.Method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return false
	}
	return kithttp.RouteKey(r) != "POST /api/risk/monitor/evaluate"
}

// requireAuth 是 fail-closed 闸：未装配 Authorizer 时返回 503（拒绝而非放行），
// 未配置鉴权时写端点 503 auth_unavailable（读端点公开），
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
			if s.limiter != nil && !s.limiter.allow(rateKey(principal), time.Now()) {
				w.Header().Set("Retry-After", "1")
				writeJSON(w, http.StatusTooManyRequests, errorBody{
					Error: "rate limit exceeded",
					Code:  "rate_limited",
				})
				return
			}
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), principalKey{}, principal)))
		case errors.Is(err, ErrAuthUnavailable):
			// 第 2 态：进程能起、读端点公开，但写端点明确关门（不是 401/403，
			// 因为不是调用方凭证的问题，而是服务端没配鉴权）。
			writeJSON(w, http.StatusServiceUnavailable, errorBody{
				Error: "authentication is not configured; write endpoints are closed until JWT_SECRET is set",
				Code:  "auth_unavailable",
			})
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

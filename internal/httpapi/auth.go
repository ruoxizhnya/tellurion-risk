package httpapi

import (
	"errors"
	"log"
	"net/http"
)

// 鉴权闸的失败分类。Authorizer 的实现（QT-50 ④ 的 JWT + RBAC 中间件）只负责
// 把凭证 / 角色判定映射到这两个哨兵；本仓不另定鉴权契约（ARCHITECTURE.md §9.4）。
var (
	ErrUnauthenticated = errors.New("no valid credentials")
	ErrForbidden       = errors.New("insufficient role")
)

// Authorizer 是监控端点的鉴权闸。nil = 闸未装配，端点必须拒绝服务。
type Authorizer interface {
	Authorize(r *http.Request) error
}

// WithAuthorizer 装配监控端点的鉴权闸。
func WithAuthorizer(a Authorizer) Option {
	return func(s *Server) { s.authorizer = a }
}

// requireAuth 是 fail-closed 闸：未装配 Authorizer 时返回 503（拒绝而非放行），
// 凭证缺失 401、角色不足 403；只有 Authorize 返回 nil 才进入 handler。
func (s *Server) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.authorizer == nil {
			writeJSON(w, http.StatusServiceUnavailable, errorBody{
				Error: "monitor endpoints require an authorizer; none is configured",
				Code:  "auth_unavailable",
			})
			return
		}
		switch err := s.authorizer.Authorize(r); {
		case err == nil:
			next.ServeHTTP(w, r)
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

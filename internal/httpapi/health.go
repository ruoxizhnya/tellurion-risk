package httpapi

import (
	"context"
	"math"
	"net/http"
	"sync"
	"time"
)

// 默认限流量级（G1）：单人自托管「不打挂自己」——50 rps、突发 200。
// 探针（`/healthz`、`/readyz`）不经过限流；429 与 401/403 用不同 code，调用方
// 不会把「被限流」读成「未登录」（core 侧 AUD-57 的同型坑）。
const (
	defaultRatePerSecond = 50
	defaultRateBurst     = 200
)

type statusBody struct {
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`
	Store  string `json:"store,omitempty"`
}

// healthz 只证明进程活着：不读库、不看鉴权配置（探针不能因配置缺失而红）。
func (s *Server) healthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, statusBody{Status: "ok"})
}

// readyz 证明「能干活」：PG 可连 + 迁移已执行（由注入的 readiness 决定）。
// 未注入 readiness = 内存档（开发用），就绪等价于进程活着。
func (s *Server) readyz(w http.ResponseWriter, r *http.Request) {
	if s.readiness == nil {
		writeJSON(w, http.StatusOK, statusBody{Status: "ready", Store: "memory"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := s.readiness(ctx); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, statusBody{Status: "not_ready", Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, statusBody{Status: "ready"})
}

// rateLimiter 是按键（调用方身份）的令牌桶。不引新依赖，够「不打挂自己」用。
type rateLimiter struct {
	mu      sync.Mutex
	rate    float64
	burst   float64
	buckets map[string]*tokenBucket
}

type tokenBucket struct {
	tokens float64
	last   time.Time
}

func newRateLimiter(perSecond, burst float64) *rateLimiter {
	return &rateLimiter{rate: perSecond, burst: burst, buckets: make(map[string]*tokenBucket)}
}

func (l *rateLimiter) allow(key string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	b, ok := l.buckets[key]
	if !ok {
		b = &tokenBucket{tokens: l.burst, last: now}
		l.buckets[key] = b
	}
	if elapsed := now.Sub(b.last).Seconds(); elapsed > 0 {
		b.tokens = math.Min(l.burst, b.tokens+elapsed*l.rate)
		b.last = now
	}
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

// rateKey 是限流键：同一个人的所有请求共享一个桶；open-access 共用一个桶。
func rateKey(p Principal) string {
	if p.Username != "" {
		return "user:" + p.Username
	}
	if p.Subject != "" {
		return "sub:" + p.Subject
	}
	return "open-access"
}

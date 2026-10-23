// Package breaker 是 X3d 熔断指令的领域实现。
//
// 熔断指令由监控层在越限时下发（本切片只做接收与幂等留痕，执行方与 ACK 等升格 ADR）。
// 幂等键是 breaker_id：同键同载荷重放 → applied=false（already_applied，视为成功）；
// 同键不同载荷 → ErrIdempotencyConflict（409），不许静默成功。
package breaker

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"tellurion-risk/internal/rule"
)

var (
	// ErrInvalidParams 指令缺 breaker_id / ts 等必填字段。
	ErrInvalidParams = errors.New("breaker command is invalid")
	// ErrInvalidAction 动作不在冻结枚举。
	ErrInvalidAction = errors.New("breaker action is not in the frozen enum")
	// ErrInvalidScope 作用域不在冻结枚举（复用 rule.Scope）。
	ErrInvalidScope = errors.New("breaker scope is not in the frozen enum")
	// ErrIdempotencyConflict 同 breaker_id 不同载荷。
	ErrIdempotencyConflict = errors.New("breaker_id already exists with different payload")
)

// Action 是冻结的熔断动作枚举。新增动作 = MINOR，旧执行方必须 fail-closed 拒收未知动作。
type Action string

const (
	// ActionHalt 停机（停止新开仓 / 实盘停机）。
	ActionHalt Action = "halt"
	// ActionCancelAll 撤销全部未成交挂单。
	ActionCancelAll Action = "cancel_all"
)

var actions = map[Action]struct{}{
	ActionHalt:      {},
	ActionCancelAll: {},
}

// Command 是 `POST /api/risk/breakers` 的 wire 形状。
//
// 幂等载荷身份 = Action + Scope + Reason，**不含** BreakerID 与 Ts：
// 同一条指令重试时客户端可能重新生成 ts，不能因此判成「不同载荷」。
type Command struct {
	BreakerID string     `json:"breaker_id"`
	Action    Action     `json:"action"`
	Scope     rule.Scope `json:"scope"`
	Reason    string     `json:"reason,omitempty"`
	Ts        time.Time  `json:"ts"`
}

// Validate 校验枚举与必填字段。未知 action / scope 一律 fail-closed 拒收。
func (c Command) Validate() error {
	if strings.TrimSpace(c.BreakerID) == "" {
		return fmt.Errorf("%w: breaker_id is required", ErrInvalidParams)
	}
	if _, ok := actions[c.Action]; !ok {
		return fmt.Errorf("%w: %q", ErrInvalidAction, c.Action)
	}
	if !c.Scope.Valid() {
		return fmt.Errorf("%w: %q", ErrInvalidScope, c.Scope)
	}
	if c.Ts.IsZero() {
		return fmt.Errorf("%w: ts is required", ErrInvalidParams)
	}
	return nil
}

// SamePayload 比对幂等载荷身份（Action + Scope + Reason，不含 BreakerID / Ts）。
func (c Command) SamePayload(other Command) bool {
	return c.Action == other.Action && c.Scope == other.Scope && c.Reason == other.Reason
}

// Store 是熔断指令的持久化端口。PutBreaker 返回 created=false 表示同键同载荷已存在。
type Store interface {
	PutBreaker(ctx context.Context, cmd Command, receivedAt time.Time) (created bool, err error)
}

// Memory 是进程内 Store（测试 / 本地开发）。
type Memory struct {
	mu   sync.Mutex
	cmds map[string]Command
}

// NewMemory 构造空的内存 Store。
func NewMemory() *Memory {
	return &Memory{cmds: make(map[string]Command)}
}

// PutBreaker 按 breaker_id 幂等写入。
func (m *Memory) PutBreaker(_ context.Context, cmd Command, _ time.Time) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if existing, ok := m.cmds[cmd.BreakerID]; ok {
		if !existing.SamePayload(cmd) {
			return false, fmt.Errorf("%w: %s", ErrIdempotencyConflict, cmd.BreakerID)
		}
		return false, nil
	}
	m.cmds[cmd.BreakerID] = cmd
	return true, nil
}

// Options 构造 Service。
type Options struct {
	Store Store
	// Now 可注入（测试固定时间）；零值取 time.Now。
	Now func() time.Time
}

// Service 是熔断指令的领域门面。
type Service struct {
	store Store
	now   func() time.Time
}

// New 构造 Service。
func New(opts Options) (*Service, error) {
	if opts.Store == nil {
		return nil, errors.New("breaker: store is required")
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	return &Service{store: opts.Store, now: opts.Now}, nil
}

// Apply 校验并幂等落库。created=true ⇒ applied；false ⇒ already_applied（视为成功）。
func (s *Service) Apply(ctx context.Context, cmd Command) (bool, error) {
	if err := cmd.Validate(); err != nil {
		return false, err
	}
	return s.store.PutBreaker(ctx, cmd, s.now().UTC())
}

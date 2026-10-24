// Package event 是 X3c 裁决事件回传的领域实现。
//
// 事件由内核 / 实盘在 run 后异步批量回传（非热路径），本包只做两件事：
// 逐条校验 + 按去重键 (run_id, order_id, event_seq) 幂等落库。
// 批量部分失败不整体拒绝：调用方拿到 207 + 逐条明细，只重发失败项。
package event

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ErrInvalidParams 事件不合法（缺 run_id / order_id、event_seq 为负、ts 缺失等）。
var ErrInvalidParams = errors.New("risk event is invalid")

// Event 是事件回传的 wire 形状。未知字段由 Service 保留在 Record.Raw 里
// （审计原文进 payload 的落库路径见 internal/pgstore），不丢字段。
type Event struct {
	RunID    string          `json:"run_id"`
	OrderID  string          `json:"order_id"`
	EventSeq int64           `json:"event_seq"`
	Symbol   string          `json:"symbol,omitempty"`
	Verdict  string          `json:"verdict,omitempty"`
	Reason   string          `json:"reason,omitempty"`
	Payload  json.RawMessage `json:"payload,omitempty"`
	Ts       time.Time       `json:"ts"`
}

// Validate 校验去重键与必填字段。未知字段不参与校验，也不丢弃。
func (e Event) Validate() error {
	if strings.TrimSpace(e.RunID) == "" {
		return fmt.Errorf("%w: run_id is required", ErrInvalidParams)
	}
	if strings.TrimSpace(e.OrderID) == "" {
		return fmt.Errorf("%w: order_id is required", ErrInvalidParams)
	}
	if e.EventSeq < 0 {
		return fmt.Errorf("%w: event_seq must be >= 0", ErrInvalidParams)
	}
	if e.Ts.IsZero() {
		return fmt.Errorf("%w: ts is required", ErrInvalidParams)
	}
	if len(e.Payload) > 0 && !json.Valid(e.Payload) {
		return fmt.Errorf("%w: payload is not valid JSON", ErrInvalidParams)
	}
	return nil
}

// Record 是真正落库的一行：规范化后的字段 + 原始 JSON（审计原文）+ 接收时间。
type Record struct {
	Event
	Raw        json.RawMessage
	ReceivedAt time.Time
}

// Failure 是批量里的单条失败项（index = 请求数组下标）。
type Failure struct {
	Index   int    `json:"index"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Result 是一次批量回传的结果。Failed 为空 = 全部成功（HTTP 200）。
type Result struct {
	Accepted   int
	Duplicates int
	Failed     []Failure
}

// Store 是事件落库端口。PutEvents 必须原子：整批要么全写入，要么全失败；
// 已存在的去重键跳过并计入 duplicates。
type Store interface {
	PutEvents(ctx context.Context, records []Record) (accepted, duplicates int, err error)
}

// Memory 是进程内 Store（测试 / 本地开发）。
type Memory struct {
	mu      sync.Mutex
	seen    map[string]struct{}
	records []Record
}

// NewMemory 构造空的内存 Store。
func NewMemory() *Memory {
	return &Memory{seen: make(map[string]struct{})}
}

// PutEvents 按 (run_id, order_id, event_seq) 去重后追加。
func (m *Memory) PutEvents(_ context.Context, records []Record) (int, int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	accepted, duplicates := 0, 0
	for _, rec := range records {
		key := dedupeKey(rec.Event)
		if _, ok := m.seen[key]; ok {
			duplicates++
			continue
		}
		m.seen[key] = struct{}{}
		rec.Raw = append(json.RawMessage(nil), rec.Raw...)
		rec.Payload = append(json.RawMessage(nil), rec.Payload...)
		m.records = append(m.records, rec)
		accepted++
	}
	return accepted, duplicates, nil
}

// records snapshot 仅供同包测试断言（不属于 Store 端口）。
func (m *Memory) stored() []Record {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Record, len(m.records))
	copy(out, m.records)
	return out
}

func dedupeKey(e Event) string {
	return e.RunID + "\x00" + e.OrderID + "\x00" + strconv.FormatInt(e.EventSeq, 10)
}

// Options 构造 Service。
type Options struct {
	Store Store
	// Reader 是监控聚合的读取端口；零值时若 Store 同时实现 Reader 则自动复用。
	Reader Reader
	// Now 可注入（测试固定时间）；零值取 time.Now。
	Now func() time.Time
}

// Service 是事件回传的领域门面。
type Service struct {
	store  Store
	reader Reader
	now    func() time.Time
}

// New 构造 Service。
func New(opts Options) (*Service, error) {
	if opts.Store == nil {
		return nil, errors.New("event: store is required")
	}
	if opts.Reader == nil {
		if r, ok := opts.Store.(Reader); ok {
			opts.Reader = r
		}
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	return &Service{store: opts.Store, reader: opts.Reader, now: opts.Now}, nil
}

// Ingest 校验一批原始事件并落库。整批不可解析（空数组）→ ErrInvalidParams；
// 单条不合法 → 记 Failure 后继续；落库错误 → 原样返回（调用方重试整批，去重键保证幂等）。
func (s *Service) Ingest(ctx context.Context, raws []json.RawMessage) (Result, error) {
	if len(raws) == 0 {
		return Result{}, fmt.Errorf("%w: events must contain at least one event", ErrInvalidParams)
	}
	records := make([]Record, 0, len(raws))
	failed := make([]Failure, 0)
	receivedAt := s.now().UTC()
	for i, raw := range raws {
		var ev Event
		if err := json.Unmarshal(raw, &ev); err != nil {
			failed = append(failed, Failure{Index: i, Code: "invalid_params", Message: "event is not a JSON object"})
			continue
		}
		if err := ev.Validate(); err != nil {
			failed = append(failed, Failure{Index: i, Code: "invalid_params", Message: err.Error()})
			continue
		}
		records = append(records, Record{
			Event:      ev,
			Raw:        append(json.RawMessage(nil), raw...),
			ReceivedAt: receivedAt,
		})
	}

	accepted, duplicates := 0, 0
	if len(records) > 0 {
		var err error
		accepted, duplicates, err = s.store.PutEvents(ctx, records)
		if err != nil {
			return Result{}, err
		}
	}
	return Result{Accepted: accepted, Duplicates: duplicates, Failed: failed}, nil
}

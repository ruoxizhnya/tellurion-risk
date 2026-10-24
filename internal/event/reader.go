package event

import (
	"context"
	"encoding/json"
	"sort"
	"time"
)

// Query 选择 risk_events 的行供监控聚合使用。零值 From/To = 不设边界，
// Limit <= 0 = 不限制；监控 Service 总会显式给全（窗口 + 上限）。
type Query struct {
	From    time.Time
	To      time.Time
	RunID   string
	OrderID string
	Symbol  string
	Verdict string
	Limit   int
}

// Reader 是 risk_events 的只读端口（监控聚合的数据来源）。
// Memory 与 pgstore 都实现它；读不到数据时必须返回错误，不许拿空集冒充「没有敞口」。
type Reader interface {
	QueryEvents(ctx context.Context, q Query) (records []Record, truncated bool, err error)
}

// QueryEvents 按事件时间过滤并按时间倒序返回，limit+1 探测截断。
func (m *Memory) QueryEvents(_ context.Context, q Query) ([]Record, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	out := make([]Record, 0, len(m.records))
	for _, rec := range m.records {
		if !q.From.IsZero() && rec.Ts.Before(q.From) {
			continue
		}
		if !q.To.IsZero() && rec.Ts.After(q.To) {
			continue
		}
		if q.RunID != "" && rec.RunID != q.RunID {
			continue
		}
		if q.OrderID != "" && rec.OrderID != q.OrderID {
			continue
		}
		if q.Symbol != "" && rec.Symbol != q.Symbol {
			continue
		}
		if q.Verdict != "" && rec.Verdict != q.Verdict {
			continue
		}
		out = append(out, cloneRecord(rec))
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if !a.Ts.Equal(b.Ts) {
			return a.Ts.After(b.Ts)
		}
		if a.RunID != b.RunID {
			return a.RunID < b.RunID
		}
		if a.OrderID != b.OrderID {
			return a.OrderID < b.OrderID
		}
		return a.EventSeq < b.EventSeq
	})

	truncated := false
	if q.Limit > 0 && len(out) > q.Limit {
		out = out[:q.Limit]
		truncated = true
	}
	return out, truncated, nil
}

func cloneRecord(rec Record) Record {
	rec.Raw = append(json.RawMessage(nil), rec.Raw...)
	rec.Payload = append(json.RawMessage(nil), rec.Payload...)
	return rec
}

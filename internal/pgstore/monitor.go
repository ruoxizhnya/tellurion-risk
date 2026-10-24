package pgstore

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"tellurion-risk/internal/event"
)

var _ event.Reader = (*Store)(nil)

// QueryEvents 读取 risk_events 供监控聚合使用。时间窗按 event_ts（调用方时间），
// 排序固定为 event_ts DESC + 去重键，保证同参数两次查询顺序一致。
func (s *Store) QueryEvents(ctx context.Context, q event.Query) ([]event.Record, bool, error) {
	query := `SELECT run_id, order_id, event_seq, symbol, verdict, reason,
		payload, event_ts, raw, received_at
		FROM risk.risk_events`
	var (
		conds []string
		args  []any
	)
	add := func(cond string, value any) {
		args = append(args, value)
		conds = append(conds, fmt.Sprintf(cond, len(args)))
	}
	if !q.From.IsZero() {
		add(`event_ts >= $%d`, q.From)
	}
	if !q.To.IsZero() {
		add(`event_ts <= $%d`, q.To)
	}
	if q.RunID != "" {
		add(`run_id = $%d`, q.RunID)
	}
	if q.OrderID != "" {
		add(`order_id = $%d`, q.OrderID)
	}
	if q.Symbol != "" {
		add(`symbol = $%d`, q.Symbol)
	}
	if q.Verdict != "" {
		add(`verdict = $%d`, q.Verdict)
	}
	if len(conds) > 0 {
		query += ` WHERE ` + strings.Join(conds, ` AND `)
	}
	query += ` ORDER BY event_ts DESC, run_id, order_id, event_seq`

	limit := q.Limit
	if limit > 0 {
		args = append(args, limit+1)
		query += fmt.Sprintf(` LIMIT $%d`, len(args))
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()

	out := make([]event.Record, 0)
	for rows.Next() {
		rec, err := scanRecord(rows)
		if err != nil {
			return nil, false, err
		}
		out = append(out, rec)
	}
	if err := rows.Err(); err != nil {
		return nil, false, err
	}

	truncated := false
	if limit > 0 && len(out) > limit {
		out = out[:limit]
		truncated = true
	}
	return out, truncated, nil
}

func scanRecord(row rowScanner) (event.Record, error) {
	var (
		rec     event.Record
		payload []byte
		raw     []byte
	)
	if err := row.Scan(&rec.RunID, &rec.OrderID, &rec.EventSeq, &rec.Symbol, &rec.Verdict,
		&rec.Reason, &payload, &rec.Ts, &raw, &rec.ReceivedAt); err != nil {
		return event.Record{}, err
	}
	if payload != nil {
		rec.Payload = append(json.RawMessage(nil), payload...)
	}
	rec.Raw = append(json.RawMessage(nil), raw...)
	return rec, nil
}

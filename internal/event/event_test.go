package event

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"testing"
	"time"
)

func newService(t *testing.T) (*Service, *Memory) {
	t.Helper()
	store := NewMemory()
	svc, err := New(Options{
		Store: store,
		Now:   func() time.Time { return time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return svc, store
}

func rawEvent(seq int, extra string) json.RawMessage {
	return rawEventFor("ord-1", seq, extra)
}

func rawEventFor(orderID string, seq int, extra string) json.RawMessage {
	return json.RawMessage(`{"run_id":"run-1","order_id":"` + orderID + `","event_seq":` +
		strconv.Itoa(seq) + `,"verdict":"allowed","ts":"2026-10-24T11:59:00Z"` + extra + `}`)
}

func TestIngest_DedupesByRunOrderSeq(t *testing.T) {
	svc, store := newService(t)
	ctx := context.Background()

	res, err := svc.Ingest(ctx, []json.RawMessage{
		rawEvent(1, ""),
		rawEvent(1, ""),             // 同键 → duplicate
		rawEventFor("ord-2", 1, ""), // 同 run/seq、不同 order_id → 不同键，必须接受
		rawEvent(2, ""),
	})
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	if res.Accepted != 3 || res.Duplicates != 1 || len(res.Failed) != 0 {
		t.Fatalf("result = %+v, want accepted=3 duplicates=1 no failures", res)
	}

	// 同一批重放：全部是 duplicate，不重复落库。
	res, err = svc.Ingest(ctx, []json.RawMessage{
		rawEvent(1, ""), rawEventFor("ord-2", 1, ""), rawEvent(2, ""),
	})
	if err != nil {
		t.Fatalf("replay Ingest: %v", err)
	}
	if res.Accepted != 0 || res.Duplicates != 3 {
		t.Fatalf("replay result = %+v, want accepted=0 duplicates=3", res)
	}
	if got := len(store.stored()); got != 3 {
		t.Fatalf("stored rows = %d, want 3", got)
	}
}

func TestIngest_PartialFailureKeepsValidItems(t *testing.T) {
	svc, store := newService(t)
	ctx := context.Background()

	res, err := svc.Ingest(ctx, []json.RawMessage{
		rawEvent(1, ""),
		json.RawMessage(`{"run_id":"run-1","order_id":"ord-1","event_seq":2,"ts":"2026-10-24T11:59:00Z","unexpected":true}`), // 合法：未知字段保留
		json.RawMessage(`{"order_id":"ord-1","event_seq":3,"ts":"2026-10-24T11:59:00Z"}`),                                    // 缺 run_id
		json.RawMessage(`[1,2]`), // 不是对象
	})
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	if res.Accepted != 2 || res.Duplicates != 0 {
		t.Fatalf("result = %+v, want accepted=2", res)
	}
	if len(res.Failed) != 2 || res.Failed[0].Index != 2 || res.Failed[1].Index != 3 {
		t.Fatalf("failures = %+v, want indexes 2 and 3", res.Failed)
	}
	for _, f := range res.Failed {
		if f.Code != "invalid_params" || f.Message == "" {
			t.Fatalf("failure must carry code+message: %+v", f)
		}
	}

	// 未知字段不得丢弃：raw 保留原文。
	stored := store.stored()
	if len(stored) != 2 {
		t.Fatalf("stored rows = %d, want 2", len(stored))
	}
	var rawMap map[string]any
	if err := json.Unmarshal(stored[1].Raw, &rawMap); err != nil {
		t.Fatalf("stored raw: %v", err)
	}
	if rawMap["unexpected"] != true {
		t.Fatalf("unknown field must be preserved in raw, got %s", stored[1].Raw)
	}
	if stored[0].ReceivedAt.IsZero() {
		t.Fatal("received_at must be stamped")
	}
}

func TestIngest_RejectsEmptyBatch(t *testing.T) {
	svc, _ := newService(t)
	if _, err := svc.Ingest(context.Background(), nil); !errors.Is(err, ErrInvalidParams) {
		t.Fatalf("empty batch: want ErrInvalidParams, got %v", err)
	}
	if _, err := svc.Ingest(context.Background(), []json.RawMessage{}); !errors.Is(err, ErrInvalidParams) {
		t.Fatalf("empty slice: want ErrInvalidParams, got %v", err)
	}
}

func TestEvent_ValidateFailsClosed(t *testing.T) {
	base := Event{RunID: "r", OrderID: "o", EventSeq: 1, Ts: time.Date(2026, 10, 24, 0, 0, 0, 0, time.UTC)}
	cases := []struct {
		name   string
		mutate func(*Event)
	}{
		{"run_id missing", func(e *Event) { e.RunID = "" }},
		{"order_id blank", func(e *Event) { e.OrderID = "  " }},
		{"negative seq", func(e *Event) { e.EventSeq = -1 }},
		{"ts zero", func(e *Event) { e.Ts = time.Time{} }},
		{"payload broken", func(e *Event) { e.Payload = json.RawMessage(`{`) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ev := base
			tc.mutate(&ev)
			if err := ev.Validate(); !errors.Is(err, ErrInvalidParams) {
				t.Fatalf("want ErrInvalidParams, got %v", err)
			}
		})
	}
	if err := base.Validate(); err != nil {
		t.Fatalf("valid event rejected: %v", err)
	}
}

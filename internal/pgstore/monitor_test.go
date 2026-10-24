package pgstore

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"tellurion-risk/internal/event"
)

// TestQueryEvents_Integration 是可选的 PG 腿（无库时 skip，不是假绿）：
// RISK_TEST_DSN='postgres://postgres:postgres@127.0.0.1:5432/tellurion_risk_test?sslmode=disable'
// go test ./internal/pgstore -run TestQueryEvents_Integration -count=1
func TestQueryEvents_Integration(t *testing.T) {
	dsn := os.Getenv("RISK_TEST_DSN")
	if dsn == "" {
		t.Skip("RISK_TEST_DSN not set")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		t.Fatalf("ping: %v", err)
	}
	if err := Migrate(ctx, db); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	runID := "test-monitor-" + time.Now().UTC().Format("20060102150405.000000000")
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), `DELETE FROM risk.risk_events WHERE run_id = $1`, runID)
	})

	base := time.Now().UTC().Truncate(time.Second)
	newRecord := func(order string, seq int64, ts time.Time, verdict, symbol, payload string) event.Record {
		raw, _ := json.Marshal(map[string]any{
			"run_id": runID, "order_id": order, "event_seq": seq, "ts": ts,
			"verdict": verdict, "symbol": symbol,
		})
		return event.Record{
			Event: event.Event{
				RunID: runID, OrderID: order, EventSeq: seq, Symbol: symbol,
				Verdict: verdict, Payload: json.RawMessage(payload), Ts: ts,
			},
			Raw: raw, ReceivedAt: base,
		}
	}
	store := New(db)
	records := []event.Record{
		newRecord("o-1", 1, base.Add(-2*time.Minute), "alert", "600000.SH", `{"rule":"drawdown","severity":"warning","message":"dd"}`),
		newRecord("o-2", 1, base.Add(-time.Minute), "alert", "000001.SZ", `{"rule":"position_concentration","severity":"critical","message":"pos"}`),
		newRecord("o-3", 1, base.Add(-3*time.Minute), "allowed", "600000.SH", `{"notional":1}`),
	}
	if accepted, duplicates, err := store.PutEvents(ctx, records); err != nil || accepted != 3 || duplicates != 0 {
		t.Fatalf("PutEvents: accepted=%d duplicates=%d err=%v", accepted, duplicates, err)
	}

	got, truncated, err := store.QueryEvents(ctx, event.Query{
		From: base.Add(-5 * time.Minute), To: base,
		RunID: runID, Verdict: "alert", Limit: 10,
	})
	if err != nil {
		t.Fatalf("QueryEvents: %v", err)
	}
	if truncated || len(got) != 2 {
		t.Fatalf("QueryEvents = %d rows truncated=%v, want 2/false", len(got), truncated)
	}
	if got[0].OrderID != "o-2" || got[1].OrderID != "o-1" {
		t.Fatalf("rows must be event_ts DESC: %+v", got)
	}
	if string(got[1].Payload) != `{"rule":"drawdown","severity":"warning","message":"dd"}` {
		t.Fatalf("payload round-trip = %s", got[1].Payload)
	}

	got, truncated, err = store.QueryEvents(ctx, event.Query{RunID: runID, Verdict: "alert", Limit: 1})
	if err != nil || !truncated || len(got) != 1 {
		t.Fatalf("limit leg: rows=%d truncated=%v err=%v", len(got), truncated, err)
	}
	got, _, err = store.QueryEvents(ctx, event.Query{RunID: runID, Symbol: "600000.SH"})
	if err != nil || len(got) != 2 {
		t.Fatalf("symbol filter: rows=%d err=%v", len(got), err)
	}
}

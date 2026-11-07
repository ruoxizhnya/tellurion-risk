package pgstore

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"tellurion-risk/internal/event"
)

// TestLegacyActorNullRow_Integration 是 QT-80 的真库腿（⑤②）：迁移只加可空列、不回填，
// 老行 actor IS NULL 时读面（QueryEvents 的 COALESCE(actor, ”)）必须不报错。
//
// 可选 PG 腿（无库时 skip，不是假绿）：
//
//	RISK_TEST_DSN='postgres://postgres@127.0.0.1:5432/tellurion_risk_test?sslmode=disable' \
//	  go test ./internal/pgstore -run TestLegacyActorNullRow -count=1
func TestLegacyActorNullRow_Integration(t *testing.T) {
	dsn := os.Getenv("RISK_TEST_DSN")
	if dsn == "" {
		t.Skip("RISK_TEST_DSN not set")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		t.Fatalf("ping: %v", err)
	}
	if err := Migrate(ctx, db); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	runID := "qt80-legacy-" + time.Now().UTC().Format("150405.000000000")
	cleanup := func() {
		_, _ = db.ExecContext(context.Background(),
			`DELETE FROM risk.risk_events WHERE run_id = $1`, runID)
	}
	cleanup()
	t.Cleanup(cleanup)

	// 直接造一条「迁移前的形状」：actor 列存在但为 NULL。
	if _, err := db.ExecContext(ctx, `INSERT INTO risk.risk_events
		(run_id, order_id, event_seq, symbol, verdict, reason, payload, event_ts, raw, received_at, actor)
		VALUES ($1, 'order-legacy', 1, 'AAPL', 'pass', '', NULL, $2, '{}'::jsonb, $2, NULL)`,
		runID, time.Now().UTC()); err != nil {
		t.Fatalf("insert legacy row: %v", err)
	}

	recs, _, err := New(db).QueryEvents(ctx, event.Query{RunID: runID})
	if err != nil {
		t.Fatalf("legacy actor=NULL row must stay readable: %v", err)
	}
	if len(recs) != 1 {
		t.Fatalf("want 1 record, got %d", len(recs))
	}
	if recs[0].Actor != "" {
		t.Fatalf("legacy actor must read as empty string, got %q", recs[0].Actor)
	}
}

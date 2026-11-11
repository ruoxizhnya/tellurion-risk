package pgstore

// 真 PG 回归腿：审批状态机在 PG Store 上必须走得通。
//
// 背景（2026-11-07 演练发现）：`Service.Approve` 先 `PutRevision`（状态覆盖），
// 再 `UpdateRule`；PG 侧的 `UpdateRule` 原本无条件 `INSERT` revision，于是
// approved→active 这一步在它刚更新过的同一行上撞 `rule_revisions_pkey`，
// HTTP 恒 409 `revision_conflict`。Memory Store 是覆盖写，单测全绿，PG 腿
// 直到真跑 `POST /rules/{id}/approve` 才暴露。
//
// 需要 `RISK_TEST_DSN`（例：
// `postgres://postgres@127.0.0.1:55432/tellurion_risk_test?sslmode=disable`）。
// 未设置时 skip，保证单仓默认 `go test ./...` 不需要外部依赖。

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"tellurion-risk/internal/rule"
)

const pgApproveParams = `{"atr_period":14,"base_multiplier":2.5,"bear_multiplier":3.5,"bull_multiplier":2.0,"sideways_multiplier":2.5}`

func TestApprovePromotesRevisionOnRealPG(t *testing.T) {
	dsn := requireTestDSN(t)
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := Migrate(ctx, db); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	ruleID := fmt.Sprintf("r-approve-pg-%d", time.Now().UnixNano())
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), `DELETE FROM risk.audit_logs WHERE rule_id = $1`, ruleID)
		_, _ = db.ExecContext(context.Background(), `DELETE FROM risk.rule_revisions WHERE rule_id = $1`, ruleID)
		_, _ = db.ExecContext(context.Background(), `DELETE FROM risk.rule_defs WHERE rule_id = $1`, ruleID)
	})

	svc, err := rule.New(rule.Options{Store: New(db)})
	if err != nil {
		t.Fatalf("rule.New: %v", err)
	}
	if _, _, _, err := svc.Create(ctx, rule.CreateRequest{
		RuleID: ruleID,
		Scope:  rule.ScopeStrategy,
		Type:   rule.RuleTypeStopLoss,
		Params: json.RawMessage(pgApproveParams),
	}); err != nil {
		t.Fatalf("Create: %v", err)
	}

	def, rev, err := svc.Approve(ctx, rule.ApproveRequest{RuleID: ruleID, Revision: 1})
	if err != nil {
		t.Fatalf("Approve on PG: %v (draft→approved 成功但 approved→active 失败即本用例要钉的回归)", err)
	}
	if rev.Status != rule.StatusActive {
		t.Fatalf("revision status = %q, want %q", rev.Status, rule.StatusActive)
	}
	if def.ActiveRevision != 1 || def.Status != rule.StatusActive {
		t.Fatalf("definition = %+v, want active_revision=1 status=active", def)
	}
}

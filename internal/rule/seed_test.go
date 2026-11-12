package rule

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

// 以下是 2026-11-12 从部署库捕获的固定向量：参数值改了会让历史 risk_rules_hash
// 失效，因此这里用绝对数值钉死，不做「大致相等」的放宽断言。
const (
	factorySnapshotHash = "84a95d7db8e10e6cd4c4324325eed03940f648071e08e1fe0043cd66c6516b2d"
)

var factoryVersionHashes = map[string]string{
	"global-position-sizing-v1": "8232c009764712d2c24c9b26acf4daf90e863ec51fbd83b947f0b0d681b305e6",
	"global-regime-v1":          "6e0141104173f84f2820e18e7e38f6eb877250a1949a8ef69ddbcd16e1ab4928",
	"global-stop-loss-v1":       "d394817e448184a57ffc971d51643d10fa2db06d886da71841a5c378818e68fb",
	"global-take-profit-v1":     "55d7a51a72c7d5cab2e350f14b2fe503394ba096b92db6cc5affe1b486839c6a",
}

// TestSeedFactory_EmptyStoreBecomesResolvable 覆盖「空库起服务」主路径：
// seed 前 resolve 必须 fail-closed（ErrNoApprovedRules），seed 后立刻可 resolve，
// 且快照 hash / 各规则 version_hash 与现库固定向量逐字节一致。
func TestSeedFactory_EmptyStoreBecomesResolvable(t *testing.T) {
	ctx := context.Background()
	svc, _ := newHarness(t)

	if _, err := svc.Resolve(ctx, ResolveRequest{Ruleset: RulesetLatest}); !errors.Is(err, ErrNoApprovedRules) {
		t.Fatalf("空库 resolve = %v, want ErrNoApprovedRules", err)
	}

	report, err := svc.SeedFactory(ctx)
	if err != nil {
		t.Fatalf("SeedFactory: %v", err)
	}
	if len(report.Created) != len(factorySeedRules) || len(report.Skipped) != 0 {
		t.Fatalf("首次 seed report = %+v, want created=%d skipped=0", report, len(factorySeedRules))
	}

	views, err := svc.List(ctx, Filter{Active: true})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(views) != len(factorySeedRules) {
		t.Fatalf("active rules = %d, want %d", len(views), len(factorySeedRules))
	}
	for _, view := range views {
		wantHash, ok := factoryVersionHashes[view.RuleID]
		if !ok {
			t.Fatalf("unexpected seeded rule %q", view.RuleID)
		}
		if view.ActiveRevision != 1 || view.Revision != 1 || view.Status != StatusActive {
			t.Fatalf("%s: revision=%d active=%d status=%s, want 1/1/active",
				view.RuleID, view.Revision, view.ActiveRevision, view.Status)
		}
		if view.VersionHash != wantHash {
			t.Fatalf("%s version_hash = %s, want %s", view.RuleID, view.VersionHash, wantHash)
		}
	}

	snap, err := svc.Resolve(ctx, ResolveRequest{Ruleset: RulesetLatest})
	if err != nil {
		t.Fatalf("resolve latest: %v", err)
	}
	if snap.RiskRulesHash != factorySnapshotHash {
		t.Fatalf("risk_rules_hash = %s, want %s", snap.RiskRulesHash, factorySnapshotHash)
	}
	if len(snap.Rules) != len(factorySeedRules) {
		t.Fatalf("snapshot rules = %d, want %d", len(snap.Rules), len(factorySeedRules))
	}

	// seed 与手工操作走同一状态机，且审计 actor 可区分出厂规则。
	for ruleID := range factoryVersionHashes {
		entries, err := svc.Audit(ctx, ruleID)
		if err != nil {
			t.Fatalf("Audit(%s): %v", ruleID, err)
		}
		if len(entries) != 2 {
			t.Fatalf("%s audit entries = %d, want 2 (create + approve)", ruleID, len(entries))
		}
		if entries[0].Action != ActionCreate || entries[1].Action != ActionApprove {
			t.Fatalf("%s audit actions = [%s %s], want [create approve]",
				ruleID, entries[0].Action, entries[1].Action)
		}
		for _, entry := range entries {
			if entry.Actor != FactorySeedActor {
				t.Fatalf("%s audit actor = %q, want %q", ruleID, entry.Actor, FactorySeedActor)
			}
		}
	}
}

// TestSeedFactory_ReplayIsIdempotent 覆盖「重复启动」：第二次 seed 不新建、不新增
// revision / 审计行，快照 hash 不变。
func TestSeedFactory_ReplayIsIdempotent(t *testing.T) {
	ctx := context.Background()
	svc, _ := newHarness(t)

	if _, err := svc.SeedFactory(ctx); err != nil {
		t.Fatalf("first SeedFactory: %v", err)
	}
	first, err := svc.Resolve(ctx, ResolveRequest{Ruleset: RulesetLatest})
	if err != nil {
		t.Fatalf("first resolve: %v", err)
	}

	report, err := svc.SeedFactory(ctx)
	if err != nil {
		t.Fatalf("second SeedFactory: %v", err)
	}
	if len(report.Created) != 0 || len(report.Skipped) != len(factorySeedRules) {
		t.Fatalf("second seed report = %+v, want created=0 skipped=%d", report, len(factorySeedRules))
	}

	second, err := svc.Resolve(ctx, ResolveRequest{Ruleset: RulesetLatest})
	if err != nil {
		t.Fatalf("second resolve: %v", err)
	}
	if second.RiskRulesHash != first.RiskRulesHash {
		t.Fatalf("replay changed risk_rules_hash: %s -> %s", first.RiskRulesHash, second.RiskRulesHash)
	}

	for _, req := range factorySeedRules {
		revs, err := svc.store.ListRevisions(ctx, req.RuleID)
		if err != nil {
			t.Fatalf("ListRevisions(%s): %v", req.RuleID, err)
		}
		if len(revs) != 1 {
			t.Fatalf("%s revisions = %d, want 1 after replay", req.RuleID, len(revs))
		}
		entries, err := svc.Audit(ctx, req.RuleID)
		if err != nil {
			t.Fatalf("Audit(%s): %v", req.RuleID, err)
		}
		if len(entries) != 2 {
			t.Fatalf("%s audit entries = %d, want 2 after replay", req.RuleID, len(entries))
		}
	}
	views, err := svc.List(ctx, Filter{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(views) != len(factorySeedRules) {
		t.Fatalf("rules after replay = %d, want %d (no duplicate rows)", len(views), len(factorySeedRules))
	}
}

// TestSeedFactory_DoesNotOverwriteManualChanges 覆盖两类人工改动：
//  1. seed 后人工改规则并审批 → 再次 seed 必须跳过，人工版本原样保留；
//  2. seed 前已存在同 rule_id 的人工规则 → seed 必须跳过，不覆盖、不代审批。
func TestSeedFactory_DoesNotOverwriteManualChanges(t *testing.T) {
	ctx := context.Background()

	t.Run("修改已 seed 的规则后重跑", func(t *testing.T) {
		svc, _ := newHarness(t)
		if _, err := svc.SeedFactory(ctx); err != nil {
			t.Fatalf("SeedFactory: %v", err)
		}

		const ruleID = "global-regime-v1"
		manualParams := json.RawMessage(`{"fast_ma_period":10,"slow_ma_period":30,"vol_lookback":60}`)
		_, rev, err := svc.Update(ctx, UpdateRequest{
			RuleID:       ruleID,
			BaseRevision: 1,
			Params:       manualParams,
			Actor:        "alice",
		})
		if err != nil {
			t.Fatalf("Update: %v", err)
		}
		if rev.Revision != 2 {
			t.Fatalf("manual revision = %d, want 2", rev.Revision)
		}
		if _, _, err := svc.Approve(ctx, ApproveRequest{RuleID: ruleID, Revision: 2, Actor: "alice"}); err != nil {
			t.Fatalf("Approve: %v", err)
		}
		manualSnap, err := svc.Resolve(ctx, ResolveRequest{Ruleset: RulesetLatest})
		if err != nil {
			t.Fatalf("resolve after manual change: %v", err)
		}
		if manualSnap.RiskRulesHash == factorySnapshotHash {
			t.Fatal("人工改动后快照 hash 不应等于出厂基线")
		}

		report, err := svc.SeedFactory(ctx)
		if err != nil {
			t.Fatalf("re-seed: %v", err)
		}
		if len(report.Created) != 0 || len(report.Skipped) != len(factorySeedRules) {
			t.Fatalf("re-seed report = %+v, want created=0 skipped=%d", report, len(factorySeedRules))
		}

		gotDef, err := svc.store.GetDefinition(ctx, ruleID)
		if err != nil {
			t.Fatalf("GetDefinition: %v", err)
		}
		if gotDef.HeadRevision != 2 || gotDef.ActiveRevision != 2 {
			t.Fatalf("%s head/active = %d/%d, want 2/2 (人工改动未被覆盖)",
				ruleID, gotDef.HeadRevision, gotDef.ActiveRevision)
		}
		gotRev, err := svc.store.GetRevision(ctx, ruleID, 2)
		if err != nil {
			t.Fatalf("GetRevision: %v", err)
		}
		gotParams, err := canonicalParams(gotRev.Params)
		if err != nil {
			t.Fatalf("canonicalParams: %v", err)
		}
		wantParams, _ := canonicalParams(manualParams)
		if string(gotParams) != string(wantParams) {
			t.Fatalf("%s params = %s, want %s", ruleID, gotParams, wantParams)
		}

		afterSnap, err := svc.Resolve(ctx, ResolveRequest{Ruleset: RulesetLatest})
		if err != nil {
			t.Fatalf("resolve after re-seed: %v", err)
		}
		if afterSnap.RiskRulesHash != manualSnap.RiskRulesHash {
			t.Fatalf("re-seed changed manual snapshot: %s -> %s",
				manualSnap.RiskRulesHash, afterSnap.RiskRulesHash)
		}

		// 人工动作的审计保留人工身份，seed 没有追加任何行。
		entries, err := svc.Audit(ctx, ruleID)
		if err != nil {
			t.Fatalf("Audit: %v", err)
		}
		if len(entries) != 5 {
			t.Fatalf("audit entries = %d, want 5 (seed create/approve + alice update/deprecate/approve)", len(entries))
		}
		if entries[2].Action != ActionUpdate || entries[3].Action != ActionDeprecate ||
			entries[4].Action != ActionApprove {
			t.Fatalf("manual audit actions = [%s %s %s], want [update deprecate approve]",
				entries[2].Action, entries[3].Action, entries[4].Action)
		}
		for _, entry := range entries[2:] {
			if entry.Actor != "alice" {
				t.Fatalf("manual audit actor = %q, want alice", entry.Actor)
			}
		}
	})

	t.Run("seed 前已有同 id 人工规则", func(t *testing.T) {
		svc, _ := newHarness(t)
		const ruleID = "global-stop-loss-v1"
		manualParams := json.RawMessage(`{"atr_period":14,"base_multiplier":2.5,"bear_multiplier":3.5,"bull_multiplier":2.0,"sideways_multiplier":2.5}`)
		if _, _, created, err := svc.Create(ctx, CreateRequest{
			RuleID: ruleID, Scope: ScopeGlobal, Type: RuleTypeStopLoss,
			Params: manualParams, Description: "manual before seed", Actor: "bob",
		}); err != nil || !created {
			t.Fatalf("manual Create: created=%t err=%v", created, err)
		}

		report, err := svc.SeedFactory(ctx)
		if err != nil {
			t.Fatalf("SeedFactory: %v", err)
		}
		if len(report.Created) != len(factorySeedRules)-1 {
			t.Fatalf("seed created = %v, want %d rules (占位规则被跳过)", report.Created, len(factorySeedRules)-1)
		}
		for _, id := range report.Skipped {
			if id != ruleID {
				t.Fatalf("skipped = %v, want only %s", report.Skipped, ruleID)
			}
		}

		def, err := svc.store.GetDefinition(ctx, ruleID)
		if err != nil {
			t.Fatalf("GetDefinition: %v", err)
		}
		// 人工规则仍是 draft rev1：seed 不覆盖内容，也不代替人工审批。
		if def.HeadRevision != 1 || def.ActiveRevision != 0 || def.Status != StatusDraft {
			t.Fatalf("%s head/active/status = %d/%d/%s, want 1/0/draft",
				ruleID, def.HeadRevision, def.ActiveRevision, def.Status)
		}
	})
}

// TestSeedFactory_RecreatesDeletedRuleOnly 覆盖「删掉一条 seed 规则后重启会补回，
// 但不动其它」：白盒删除 Memory 中的一条规则，重跑 seed 只补这一条，
// 其余规则的 revision / 审计 / version_hash 全部不变。
func TestSeedFactory_RecreatesDeletedRuleOnly(t *testing.T) {
	ctx := context.Background()
	svc, store := newHarness(t)
	if _, err := svc.SeedFactory(ctx); err != nil {
		t.Fatalf("SeedFactory: %v", err)
	}

	const deleted = "global-stop-loss-v1"
	before := map[string]string{}
	for _, req := range factorySeedRules {
		if req.RuleID == deleted {
			continue
		}
		view, err := svc.store.GetDefinition(ctx, req.RuleID)
		if err != nil {
			t.Fatalf("GetDefinition(%s): %v", req.RuleID, err)
		}
		rev, err := svc.store.GetRevision(ctx, req.RuleID, view.ActiveRevision)
		if err != nil {
			t.Fatalf("GetRevision(%s): %v", req.RuleID, err)
		}
		before[req.RuleID] = rev.VersionHash
	}

	store.mu.Lock()
	delete(store.defs, deleted)
	delete(store.revisions, deleted)
	store.mu.Unlock()

	report, err := svc.SeedFactory(ctx)
	if err != nil {
		t.Fatalf("re-seed after delete: %v", err)
	}
	if len(report.Created) != 1 || report.Created[0] != deleted {
		t.Fatalf("created = %v, want [%s]", report.Created, deleted)
	}
	if len(report.Skipped) != len(factorySeedRules)-1 {
		t.Fatalf("skipped = %v, want %d 条其它规则", report.Skipped, len(factorySeedRules)-1)
	}

	for ruleID, wantHash := range before {
		def, err := svc.store.GetDefinition(ctx, ruleID)
		if err != nil {
			t.Fatalf("other rule %s lost after re-seed: %v", ruleID, err)
		}
		rev, err := svc.store.GetRevision(ctx, ruleID, def.ActiveRevision)
		if err != nil {
			t.Fatalf("GetRevision(%s): %v", ruleID, err)
		}
		if rev.VersionHash != wantHash {
			t.Fatalf("other rule %s changed: %s -> %s", ruleID, wantHash, rev.VersionHash)
		}
		entries, err := svc.Audit(ctx, ruleID)
		if err != nil {
			t.Fatalf("Audit(%s): %v", ruleID, err)
		}
		if len(entries) != 2 {
			t.Fatalf("other rule %s audit entries = %d, want 2 (未被重写)", ruleID, len(entries))
		}
	}

	def, err := svc.store.GetDefinition(ctx, deleted)
	if err != nil {
		t.Fatalf("deleted rule not re-created: %v", err)
	}
	if def.ActiveRevision != 1 || def.Status != StatusActive {
		t.Fatalf("re-created rule active=%d status=%s, want 1/active", def.ActiveRevision, def.Status)
	}
	rev, err := svc.store.GetRevision(ctx, deleted, 1)
	if err != nil {
		t.Fatalf("GetRevision(%s): %v", deleted, err)
	}
	if rev.VersionHash != factoryVersionHashes[deleted] {
		t.Fatalf("re-created %s version_hash = %s, want %s",
			deleted, rev.VersionHash, factoryVersionHashes[deleted])
	}

	snap, err := svc.Resolve(ctx, ResolveRequest{Ruleset: RulesetLatest})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if snap.RiskRulesHash != factorySnapshotHash {
		t.Fatalf("restored risk_rules_hash = %s, want %s", snap.RiskRulesHash, factorySnapshotHash)
	}
}

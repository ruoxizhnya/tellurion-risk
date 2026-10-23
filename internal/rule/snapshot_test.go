package rule

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// ─── SnapshotHash ────────────────────────────────────────────────

func snapshotRules() []SnapshotRule {
	return []SnapshotRule{
		{
			RuleID:      "r-sl",
			Revision:    1,
			Scope:       ScopeStrategy,
			Type:        RuleTypeStopLoss,
			Params:      json.RawMessage(`{"pct":2.5}`),
			VersionHash: strings.Repeat("a", 64),
		},
		{
			RuleID:      "r-global",
			Revision:    3,
			Scope:       ScopeGlobal,
			Type:        RuleTypePositionSizing,
			Params:      json.RawMessage(`{"max_pct":10}`),
			VersionHash: strings.Repeat("b", 64),
		},
	}
}

func TestSnapshotHash_IsContentAddressed(t *testing.T) {
	rules := snapshotRules()
	h, err := SnapshotHash(rules)
	if err != nil {
		t.Fatalf("SnapshotHash: %v", err)
	}
	if len(h) != 64 || strings.ToLower(h) != h {
		t.Fatalf("hash must be 64 lowercase hex, got %q", h)
	}

	// 顺序不参与：同一内容任意排列同 hash。
	reversed := []SnapshotRule{rules[1], rules[0]}
	h2, err := SnapshotHash(reversed)
	if err != nil {
		t.Fatalf("SnapshotHash(reversed): %v", err)
	}
	if h2 != h {
		t.Fatalf("order must not affect hash:\n a=%s\n b=%s", h, h2)
	}

	// params 书写差异不参与（canonical 化）。
	unsorted := snapshotRules()
	unsorted[0].Params = json.RawMessage(`{  "pct":  2.5 }`)
	h3, err := SnapshotHash(unsorted)
	if err != nil {
		t.Fatalf("SnapshotHash(unsorted params): %v", err)
	}
	if h3 != h {
		t.Fatalf("params whitespace must not affect hash")
	}

	// 逐字段变更必须换 hash。
	changes := map[string]func(*[]SnapshotRule){
		"rule_id":      func(rs *[]SnapshotRule) { (*rs)[0].RuleID = "r-other" },
		"revision":     func(rs *[]SnapshotRule) { (*rs)[0].Revision = 2 },
		"scope":        func(rs *[]SnapshotRule) { (*rs)[0].Scope = ScopeAccount },
		"type":         func(rs *[]SnapshotRule) { (*rs)[0].Type = RuleTypeRegime },
		"params":       func(rs *[]SnapshotRule) { (*rs)[0].Params = json.RawMessage(`{"pct":3}`) },
		"version_hash": func(rs *[]SnapshotRule) { (*rs)[0].VersionHash = strings.Repeat("c", 64) },
		"rule removed": func(rs *[]SnapshotRule) { *rs = (*rs)[:1] },
		"rule added": func(rs *[]SnapshotRule) {
			*rs = append(*rs, SnapshotRule{
				RuleID: "r-new", Revision: 1, Scope: ScopeSymbol, Type: RuleTypeTakeProfit,
				Params: json.RawMessage(`{"x":1}`), VersionHash: strings.Repeat("d", 64),
			})
		},
	}
	for name, mutate := range changes {
		t.Run(name, func(t *testing.T) {
			mutated := snapshotRules()
			mutate(&mutated)
			hm, err := SnapshotHash(mutated)
			if err != nil {
				t.Fatalf("SnapshotHash: %v", err)
			}
			if hm == h {
				t.Fatalf("changing %s must change snapshot hash", name)
			}
		})
	}
}

func TestSnapshotHash_FailsClosedOnInvalidInput(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*[]SnapshotRule)
		want   error
	}{
		{"empty", func(rs *[]SnapshotRule) { *rs = nil }, ErrInvalidParams},
		{"duplicate rule_id", func(rs *[]SnapshotRule) { *rs = append(*rs, (*rs)[0]) }, ErrInvalidParams},
		{"rule_id empty", func(rs *[]SnapshotRule) { (*rs)[0].RuleID = "" }, ErrInvalidParams},
		{"revision zero", func(rs *[]SnapshotRule) { (*rs)[0].Revision = 0 }, ErrInvalidParams},
		{"type unknown", func(rs *[]SnapshotRule) { (*rs)[0].Type = "magic" }, ErrInvalidRuleType},
		{"scope unknown", func(rs *[]SnapshotRule) { (*rs)[0].Scope = "planet" }, ErrInvalidScope},
		{"version_hash missing", func(rs *[]SnapshotRule) { (*rs)[0].VersionHash = "" }, ErrInvalidParams},
		{"params array", func(rs *[]SnapshotRule) { (*rs)[0].Params = json.RawMessage(`[1]`) }, ErrInvalidParams},
		{"params broken", func(rs *[]SnapshotRule) { (*rs)[0].Params = json.RawMessage(`{"pct":`) }, ErrInvalidParams},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rules := snapshotRules()
			tc.mutate(&rules)
			if _, err := SnapshotHash(rules); !errors.Is(err, tc.want) {
				t.Fatalf("want %v, got %v", tc.want, err)
			}
		})
	}
}

// ─── resolve / snapshots（Service 层） ───────────────────────────

func mustApprove(t *testing.T, svc *Service, ruleID string, revision int) {
	t.Helper()
	if _, _, err := svc.Approve(context.Background(), ApproveRequest{RuleID: ruleID, Revision: revision}); err != nil {
		t.Fatalf("Approve(%s@%d): %v", ruleID, revision, err)
	}
}

func mustResolveLatest(t *testing.T, svc *Service) Snapshot {
	t.Helper()
	snap, err := svc.Resolve(context.Background(), ResolveRequest{Ruleset: RulesetLatest})
	if err != nil {
		t.Fatalf("Resolve(latest): %v", err)
	}
	return snap
}

func TestResolveLatest_OnlyActiveRevisionsEnterSnapshot(t *testing.T) {
	svc, store := newHarness(t)
	mustCreate(t, svc, CreateRequest{RuleID: "r-sl", Scope: ScopeStrategy, Type: RuleTypeStopLoss, Params: json.RawMessage(`{"pct":2.5}`)})
	mustCreate(t, svc, CreateRequest{RuleID: "r-global", Scope: ScopeGlobal, Type: RuleTypePositionSizing, Params: json.RawMessage(`{"max_pct":10}`)})
	mustApprove(t, svc, "r-sl", 1)
	mustApprove(t, svc, "r-global", 1)

	// r-sl 出一条 draft 修订（head=2），生效指针仍在 rev1：draft 永不进快照。
	if _, _, err := svc.Update(context.Background(), UpdateRequest{RuleID: "r-sl", BaseRevision: 1, Params: json.RawMessage(`{"pct":5}`)}); err != nil {
		t.Fatalf("Update: %v", err)
	}

	snap := mustResolveLatest(t, svc)
	if len(snap.Rules) != 2 {
		t.Fatalf("snapshot must contain exactly 2 active rules, got %d (%+v)", len(snap.Rules), snap.Rules)
	}
	byID := map[string]SnapshotRule{}
	for _, r := range snap.Rules {
		byID[r.RuleID] = r
	}
	if got := byID["r-sl"]; got.Revision != 1 {
		t.Fatalf("r-sl active revision = %d, want 1 (draft head must not enter snapshot)", got.Revision)
	}
	if got := byID["r-global"]; got.Revision != 1 {
		t.Fatalf("r-global active revision = %d, want 1", got.Revision)
	}
	if len(snap.RiskRulesHash) != 64 {
		t.Fatalf("risk_rules_hash must be 64 hex, got %q", snap.RiskRulesHash)
	}

	// 快照已持久化：按 hash 反查（不变量 1：risk_rules_hash 反查唯一快照）。
	again, err := store.GetSnapshot(context.Background(), snap.RiskRulesHash)
	if err != nil {
		t.Fatalf("GetSnapshot: %v", err)
	}
	if !sameSnapshotRules(again.Rules, snap.Rules) {
		t.Fatalf("persisted snapshot content mismatch")
	}

	// active 集没变 ⇒ 同 hash，重复 resolve 幂等。
	snap2 := mustResolveLatest(t, svc)
	if snap2.RiskRulesHash != snap.RiskRulesHash {
		t.Fatalf("unchanged active set must resolve to same hash:\n a=%s\n b=%s",
			snap.RiskRulesHash, snap2.RiskRulesHash)
	}
}

func TestResolveLatest_FailsClosedWithoutApprovedRules(t *testing.T) {
	svc, _ := newHarness(t)

	if _, err := svc.Resolve(context.Background(), ResolveRequest{Ruleset: RulesetLatest}); !errors.Is(err, ErrNoApprovedRules) {
		t.Fatalf("empty store: want ErrNoApprovedRules, got %v", err)
	}

	// 只有 draft 也一样：未审批规则不可能生效。
	mustCreate(t, svc, CreateRequest{RuleID: "r-draft", Scope: ScopeGlobal, Type: RuleTypeStopLoss, Params: json.RawMessage(`{"pct":1}`)})
	if _, err := svc.Resolve(context.Background(), ResolveRequest{Ruleset: RulesetLatest}); !errors.Is(err, ErrNoApprovedRules) {
		t.Fatalf("draft-only store: want ErrNoApprovedRules, got %v", err)
	}
}

func TestResolve_PinnedHashNeverFallsBack(t *testing.T) {
	svc, _ := newHarness(t)
	mustCreate(t, svc, CreateRequest{RuleID: "r-sl", Scope: ScopeStrategy, Type: RuleTypeStopLoss, Params: json.RawMessage(`{"pct":2.5}`)})
	mustApprove(t, svc, "r-sl", 1)
	old := mustResolveLatest(t, svc)

	// 出新 revision 并激活：latest 前进，但历史快照仍按 hash 可复现。
	if _, _, err := svc.Update(context.Background(), UpdateRequest{RuleID: "r-sl", BaseRevision: 1, Params: json.RawMessage(`{"pct":5}`)}); err != nil {
		t.Fatalf("Update: %v", err)
	}
	mustApprove(t, svc, "r-sl", 2)
	fresh := mustResolveLatest(t, svc)
	if fresh.RiskRulesHash == old.RiskRulesHash {
		t.Fatalf("changed active set must produce a new hash")
	}
	if fresh.Rules[0].Revision != 2 {
		t.Fatalf("latest snapshot must point at rev 2, got %d", fresh.Rules[0].Revision)
	}

	pinned, err := svc.Resolve(context.Background(), ResolveRequest{Hash: old.RiskRulesHash})
	if err != nil {
		t.Fatalf("Resolve(pinned): %v", err)
	}
	if pinned.Rules[0].Revision != 1 || pinned.RiskRulesHash != old.RiskRulesHash {
		t.Fatalf("pinned resolve must reproduce the historical snapshot, got %+v", pinned.Rules)
	}

	// 未知 hash → ErrSnapshotNotFound，绝不回退 latest。
	if _, err := svc.Resolve(context.Background(), ResolveRequest{Hash: strings.Repeat("e", 64)}); !errors.Is(err, ErrSnapshotNotFound) {
		t.Fatalf("unknown hash: want ErrSnapshotNotFound, got %v", err)
	}
}

func TestResolve_RequestValidation(t *testing.T) {
	svc, _ := newHarness(t)
	cases := []ResolveRequest{
		{Hash: "h", Ruleset: RulesetLatest}, // 二选一被同时给
		{},                                  // 都不给
		{Ruleset: "yolo"},                   // ruleset 只有 latest
	}
	for _, req := range cases {
		if _, err := svc.Resolve(context.Background(), req); !errors.Is(err, ErrInvalidParams) {
			t.Fatalf("req %+v: want ErrInvalidParams, got %v", req, err)
		}
	}
}

// ─── Store 层内容寻址护栏 ─────────────────────────────────────────

func TestPutSnapshot_FailsLoudOnTamperedContent(t *testing.T) {
	svc, store := newHarness(t)
	mustCreate(t, svc, CreateRequest{RuleID: "r-sl", Scope: ScopeStrategy, Type: RuleTypeStopLoss, Params: json.RawMessage(`{"pct":2.5}`)})
	mustApprove(t, svc, "r-sl", 1)
	snap := mustResolveLatest(t, svc)

	// 声称 hash 与内容不符（写侧 derive 校验）。
	tamperedHash := snap.Clone()
	tamperedHash.RiskRulesHash = strings.Repeat("0", 64)
	if err := store.PutSnapshot(context.Background(), tamperedHash); !errors.Is(err, ErrSnapshotMismatch) {
		t.Fatalf("tampered hash: want ErrSnapshotMismatch, got %v", err)
	}

	// 同 hash 不同内容（fail-loud，不许静默覆盖）。
	tamperedRules := snap.Clone()
	tamperedRules.Rules[0].Params = json.RawMessage(`{"pct":99}`)
	if err := store.PutSnapshot(context.Background(), tamperedRules); !errors.Is(err, ErrSnapshotMismatch) {
		t.Fatalf("same hash different content: want ErrSnapshotMismatch, got %v", err)
	}

	// 同 hash 同内容重放 = no-op。
	if err := store.PutSnapshot(context.Background(), snap); err != nil {
		t.Fatalf("idempotent rewrite: %v", err)
	}
}

func TestGetSnapshot_VerifiesContentOnRead(t *testing.T) {
	svc, store := newHarness(t)
	mustCreate(t, svc, CreateRequest{RuleID: "r-sl", Scope: ScopeStrategy, Type: RuleTypeStopLoss, Params: json.RawMessage(`{"pct":2.5}`)})
	mustApprove(t, svc, "r-sl", 1)
	snap := mustResolveLatest(t, svc)

	// 直接改内存里的快照（同包测试才能做这一步）：读侧 derive 校验必须把它抓出来。
	backend := store.snapshots[snap.RiskRulesHash]
	backend.Rules[0].Params = json.RawMessage(`{"pct":42}`)
	store.snapshots[snap.RiskRulesHash] = backend

	if _, err := store.GetSnapshot(context.Background(), snap.RiskRulesHash); !errors.Is(err, ErrSnapshotMismatch) {
		t.Fatalf("read-side verification: want ErrSnapshotMismatch, got %v", err)
	}
}

// ─── ParseBundle（进程内 RiskPolicy 边界） ────────────────────────

func TestParseBundle_RoundTripAndScopeLookup(t *testing.T) {
	svc, _ := newHarness(t)
	mustCreate(t, svc, CreateRequest{RuleID: "r-sl", Scope: ScopeStrategy, Type: RuleTypeStopLoss, Params: json.RawMessage(`{"pct":2.5}`)})
	mustCreate(t, svc, CreateRequest{RuleID: "r-global", Scope: ScopeGlobal, Type: RuleTypePositionSizing, Params: json.RawMessage(`{"max_pct":10}`)})
	mustApprove(t, svc, "r-sl", 1)
	mustApprove(t, svc, "r-global", 1)
	snap := mustResolveLatest(t, svc)

	data, err := json.Marshal(snap)
	if err != nil {
		t.Fatalf("marshal snapshot: %v", err)
	}
	b, err := ParseBundle(data)
	if err != nil {
		t.Fatalf("ParseBundle: %v", err)
	}
	if b.RiskRulesHash != snap.RiskRulesHash {
		t.Fatalf("bundle hash %q != snapshot hash %q", b.RiskRulesHash, snap.RiskRulesHash)
	}

	strategy := b.ForScope(ScopeStrategy)
	if len(strategy) != 1 || strategy[0].RuleID != "r-sl" || strategy[0].Type != RuleTypeStopLoss {
		t.Fatalf("ForScope(strategy) = %+v", strategy)
	}
	if string(strategy[0].Params) != `{"pct":2.5}` {
		t.Fatalf("params must survive the boundary, got %s", strategy[0].Params)
	}

	// 找不到 scope = 没有规则（空 slice，不是 nil、不是默认限额）。
	missing := b.ForScope(ScopeSymbol)
	if missing == nil || len(missing) != 0 {
		t.Fatalf("ForScope(missing) must be an empty slice, got %#v", missing)
	}
}

func TestParseBundle_FailsClosed(t *testing.T) {
	svc, _ := newHarness(t)
	mustCreate(t, svc, CreateRequest{RuleID: "r-sl", Scope: ScopeStrategy, Type: RuleTypeStopLoss, Params: json.RawMessage(`{"pct":2.5}`)})
	mustApprove(t, svc, "r-sl", 1)
	snap := mustResolveLatest(t, svc)
	valid, err := json.Marshal(snap)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	cases := []struct {
		name string
		data []byte
		want error
	}{
		{"not json", []byte(`{`), ErrInvalidParams},
		{"empty rules", []byte(`{"risk_rules_hash":"` + strings.Repeat("a", 64) + `","rules":[]}`), ErrInvalidParams},
		{"unknown field", []byte(`{"risk_rules_hash":"x","rules":[],"extra":1}`), ErrInvalidParams},
		{"trailing content", append(valid, []byte(`{}`)...), ErrInvalidParams},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := ParseBundle(tc.data); !errors.Is(err, tc.want) {
				t.Fatalf("want %v, got %v", tc.want, err)
			}
		})
	}

	// hash 与内容不符：拒绝开跑，不回落。
	tampered := snap
	tampered.RiskRulesHash = strings.Repeat("f", 64)
	data, err := json.Marshal(tampered)
	if err != nil {
		t.Fatalf("marshal tampered: %v", err)
	}
	if _, err := ParseBundle(data); !errors.Is(err, ErrSnapshotMismatch) {
		t.Fatalf("tampered hash: want ErrSnapshotMismatch, got %v", err)
	}

	// 未知规则类型：旧读者 fail-closed 拒收（不做「忽略不认识的规则」）。
	// verifyHash 先走 SnapshotHash derive，未知类型在 derive 处即被拒。
	unknown := snap
	unknown.Rules[0].Type = "magic"
	data, err = json.Marshal(unknown)
	if err != nil {
		t.Fatalf("marshal unknown type: %v", err)
	}
	if _, err := ParseBundle(data); !errors.Is(err, ErrInvalidRuleType) {
		t.Fatalf("unknown rule type: want ErrInvalidRuleType, got %v", err)
	}
}

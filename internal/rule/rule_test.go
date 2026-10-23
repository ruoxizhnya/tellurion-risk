package rule

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func fixedNow() time.Time { return time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC) }

func newHarness(t *testing.T) (*Service, *Memory) {
	t.Helper()
	store := NewMemory()
	svc, err := New(Options{Store: store, Now: fixedNow})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return svc, store
}

func baseRevision(ruleID string) Revision {
	return Revision{
		RuleID:      ruleID,
		Revision:    1,
		Scope:       ScopeStrategy,
		Type:        RuleTypeStopLoss,
		Params:      json.RawMessage(`{"pct":2.5}`),
		Description: "default stop loss",
	}
}

func mustCreate(t *testing.T, svc *Service, req CreateRequest) (Definition, Revision) {
	t.Helper()
	def, rev, created, err := svc.Create(context.Background(), req)
	if err != nil {
		t.Fatalf("Create(%s): %v", req.RuleID, err)
	}
	if !created {
		t.Fatalf("Create(%s): expected created=true", req.RuleID)
	}
	return def, rev
}

// ─── version_hash ────────────────────────────────────────────────

func TestVersionHash_IsContentAddressed(t *testing.T) {
	a := baseRevision("r1")
	a.Params = json.RawMessage(`{"pct":2.5,"floor":0.1}`)
	b := baseRevision("r1")
	b.Params = json.RawMessage(`{  "floor": 0.1, "pct": 2.5  }`)
	b.Description = "different description"
	b.Status = StatusActive
	b.CreatedAt = fixedNow().Add(time.Hour)

	ha, err := VersionHash(a)
	if err != nil {
		t.Fatalf("VersionHash(a): %v", err)
	}
	hb, err := VersionHash(b)
	if err != nil {
		t.Fatalf("VersionHash(b): %v", err)
	}
	if ha != hb {
		t.Fatalf("same params should hash equally:\n a=%s\n b=%s", ha, hb)
	}
	if len(ha) != 64 || strings.ToLower(ha) != ha {
		t.Fatalf("hash must be 64 lowercase hex, got %q", ha)
	}

	changes := map[string]func(*Revision){
		"rule_id":  func(r *Revision) { r.RuleID = "r2" },
		"revision": func(r *Revision) { r.Revision = 2 },
		"scope":    func(r *Revision) { r.Scope = ScopeGlobal },
		"type":     func(r *Revision) { r.Type = RuleTypeRegime },
		"params":   func(r *Revision) { r.Params = json.RawMessage(`{"pct":3}`) },
	}
	for name, mutate := range changes {
		t.Run(name, func(t *testing.T) {
			c := a
			mutate(&c)
			h, err := VersionHash(c)
			if err != nil {
				t.Fatalf("VersionHash: %v", err)
			}
			if h == ha {
				t.Fatalf("changing %s must change version_hash", name)
			}
		})
	}
}

func TestVersionHash_FailsClosedOnInvalidInput(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*Revision)
		want   error
	}{
		{"rule_id empty", func(r *Revision) { r.RuleID = "" }, ErrInvalidParams},
		{"revision zero", func(r *Revision) { r.Revision = 0 }, ErrInvalidParams},
		{"type unknown", func(r *Revision) { r.Type = "magic" }, ErrInvalidRuleType},
		{"scope unknown", func(r *Revision) { r.Scope = "planet" }, ErrInvalidScope},
		{"params array", func(r *Revision) { r.Params = json.RawMessage(`[1]`) }, ErrInvalidParams},
		{"params null", func(r *Revision) { r.Params = json.RawMessage(`null`) }, ErrInvalidParams},
		{"params broken", func(r *Revision) { r.Params = json.RawMessage(`{"pct":`) }, ErrInvalidParams},
		{"params missing", func(r *Revision) { r.Params = nil }, ErrInvalidParams},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rev := baseRevision("r1")
			tc.mutate(&rev)
			if _, err := VersionHash(rev); !errors.Is(err, tc.want) {
				t.Fatalf("want %v, got %v", tc.want, err)
			}
		})
	}
}

func TestValidate_RejectsTrailingAndOversizedParams(t *testing.T) {
	trailing := baseRevision("r1")
	trailing.Params = json.RawMessage(`{"pct":1}{"x":2}`)
	if err := trailing.Validate(); !errors.Is(err, ErrInvalidParams) {
		t.Fatalf("trailing content: want ErrInvalidParams, got %v", err)
	}

	big := baseRevision("r1")
	value := strings.Repeat("0", MaxParamsBytes+10)
	big.Params = json.RawMessage(`{"blob":"` + value + `"}`)
	if err := big.Validate(); !errors.Is(err, ErrInvalidParams) {
		t.Fatalf("oversized params: want ErrInvalidParams, got %v", err)
	}
}

// ─── 服务语义 ────────────────────────────────────────────────────

func TestCreate_IsIdempotentOnRuleID(t *testing.T) {
	svc, _ := newHarness(t)
	req := CreateRequest{
		RuleID: "r-stop", Scope: ScopeStrategy, Type: RuleTypeStopLoss,
		Params: json.RawMessage(`{"pct":2.5}`), Description: "d",
	}
	def1, rev1 := mustCreate(t, svc, req)

	def2, rev2, created, err := svc.Create(context.Background(), req)
	if err != nil {
		t.Fatalf("replay Create: %v", err)
	}
	if created {
		t.Fatal("replay must not create a new rule")
	}
	if def2.RuleID != def1.RuleID || rev2.Revision != rev1.Revision || rev2.VersionHash != rev1.VersionHash {
		t.Fatalf("replay must return the same head: %+v vs %+v", rev2, rev1)
	}

	diff := req
	diff.Params = json.RawMessage(`{"pct":9}`)
	if _, _, _, err := svc.Create(context.Background(), diff); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("different content: want ErrIdempotencyConflict, got %v", err)
	}
}

func TestCreate_RejectsInvalidDefinitionBeforePersisting(t *testing.T) {
	cases := []struct {
		name string
		req  CreateRequest
		want error
	}{
		{"unknown type", CreateRequest{RuleID: "a", Scope: ScopeGlobal, Type: "magic", Params: json.RawMessage(`{}`)}, ErrInvalidRuleType},
		{"unknown scope", CreateRequest{RuleID: "b", Scope: "planet", Type: RuleTypeRegime, Params: json.RawMessage(`{}`)}, ErrInvalidScope},
		{"params scalar", CreateRequest{RuleID: "c", Scope: ScopeGlobal, Type: RuleTypeRegime, Params: json.RawMessage(`1`)}, ErrInvalidParams},
		{"empty rule id", CreateRequest{RuleID: "", Scope: ScopeGlobal, Type: RuleTypeRegime, Params: json.RawMessage(`{}`)}, ErrInvalidParams},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc, store := newHarness(t)
			if _, _, _, err := svc.Create(context.Background(), tc.req); !errors.Is(err, tc.want) {
				t.Fatalf("want %v, got %v", tc.want, err)
			}
			if tc.req.RuleID != "" {
				if _, err := store.GetDefinition(context.Background(), tc.req.RuleID); !errors.Is(err, ErrRuleNotFound) {
					t.Fatalf("rejected rule must not be persisted, got %v", err)
				}
			}
		})
	}
}

func TestUpdate_BumpsRevisionAndGuardsBase(t *testing.T) {
	svc, _ := newHarness(t)
	req := CreateRequest{RuleID: "r", Scope: ScopeSymbol, Type: RuleTypeTakeProfit,
		Params: json.RawMessage(`{"tiers":[]}`), Description: "keep me"}
	mustCreate(t, svc, req)

	// base_revision 必须等于 head。
	if _, _, err := svc.Update(context.Background(), UpdateRequest{
		RuleID: "r", BaseRevision: 7, Params: json.RawMessage(`{"tiers":[1]}`),
	}); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("stale base: want ErrRevisionConflict, got %v", err)
	}

	// 不传 description = 继承原描述。
	def, rev, err := svc.Update(context.Background(), UpdateRequest{
		RuleID: "r", BaseRevision: 1, Params: json.RawMessage(`{"tiers":[1]}`),
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if rev.Revision != 2 || def.HeadRevision != 2 {
		t.Fatalf("want head 2, got def %d rev %d", def.HeadRevision, rev.Revision)
	}
	if rev.Description != "keep me" {
		t.Fatalf("description should be inherited, got %q", rev.Description)
	}
	if rev.Scope != ScopeSymbol || rev.Type != RuleTypeTakeProfit {
		t.Fatalf("scope/type must be inherited: %+v", rev)
	}
	if rev.Status != StatusDraft {
		t.Fatalf("new revision must be draft, got %s", rev.Status)
	}

	empty := ""
	def, rev, err = svc.Update(context.Background(), UpdateRequest{
		RuleID: "r", BaseRevision: 2, Params: json.RawMessage(`{"tiers":[2]}`), Description: &empty,
	})
	if err != nil {
		t.Fatalf("Update with explicit empty description: %v", err)
	}
	if rev.Description != "" || def.Description != "" {
		t.Fatalf("explicit empty description must clear, got %q / %q", rev.Description, def.Description)
	}

	// 非法参数不得推进 head。
	if _, _, err := svc.Update(context.Background(), UpdateRequest{
		RuleID: "r", BaseRevision: 3, Params: json.RawMessage(`[]`),
	}); !errors.Is(err, ErrInvalidParams) {
		t.Fatalf("invalid params: want ErrInvalidParams, got %v", err)
	}
	head, err := svc.List(context.Background(), Filter{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(head) != 1 || head[0].Revision != 3 {
		t.Fatalf("head must stay 3 after rejected update: %+v", head)
	}
}

func TestApprove_StateMachineAndSupersession(t *testing.T) {
	svc, store := newHarness(t)
	ctx := context.Background()
	mustCreate(t, svc, CreateRequest{
		RuleID: "r", Scope: ScopeStrategy, Type: RuleTypeStopLoss, Params: json.RawMessage(`{"pct":2}`),
	})

	def, rev, err := svc.Approve(ctx, ApproveRequest{RuleID: "r", Revision: 1})
	if err != nil {
		t.Fatalf("Approve: %v", err)
	}
	if rev.Status != StatusActive || def.Status != StatusActive || def.ActiveRevision != 1 {
		t.Fatalf("want active@1, got def=%+v rev=%+v", def, rev)
	}
	if rev.ApprovedAt == nil {
		t.Fatal("approved_at must be set")
	}

	// 重复批准同一 revision：幂等，同 hash。
	def2, rev2, err := svc.Approve(ctx, ApproveRequest{RuleID: "r", Revision: 1})
	if err != nil {
		t.Fatalf("replay Approve: %v", err)
	}
	if rev2.VersionHash != rev.VersionHash || def2.ActiveRevision != 1 || rev2.Status != StatusActive {
		t.Fatalf("replay must be identical: %+v vs %+v", rev2, rev)
	}

	// 新 revision 激活 → 旧的 active 变 deprecated。
	if _, _, err := svc.Update(ctx, UpdateRequest{RuleID: "r", BaseRevision: 1, Params: json.RawMessage(`{"pct":3}`)}); err != nil {
		t.Fatalf("Update: %v", err)
	}
	def3, rev3, err := svc.Approve(ctx, ApproveRequest{RuleID: "r", Revision: 2})
	if err != nil {
		t.Fatalf("Approve rev2: %v", err)
	}
	if def3.ActiveRevision != 2 || rev3.Status != StatusActive {
		t.Fatalf("want active@2, got %+v", def3)
	}
	old, err := store.GetRevision(ctx, "r", 1)
	if err != nil {
		t.Fatalf("GetRevision 1: %v", err)
	}
	if old.Status != StatusDeprecated {
		t.Fatalf("superseded revision must be deprecated, got %s", old.Status)
	}

	// 已废弃 revision 不能再批准。
	if _, _, err := svc.Approve(ctx, ApproveRequest{RuleID: "r", Revision: 1}); !errors.Is(err, ErrRevisionNotDraft) {
		t.Fatalf("approve deprecated: want ErrRevisionNotDraft, got %v", err)
	}

	if _, _, err := svc.Approve(ctx, ApproveRequest{RuleID: "r", Revision: 99}); !errors.Is(err, ErrRevisionNotFound) {
		t.Fatalf("unknown revision: want ErrRevisionNotFound, got %v", err)
	}
	if _, _, err := svc.Approve(ctx, ApproveRequest{RuleID: "nope", Revision: 1}); !errors.Is(err, ErrRuleNotFound) {
		t.Fatalf("unknown rule: want ErrRuleNotFound, got %v", err)
	}
}

func TestList_EmptyIsNonNilAndFiltersWork(t *testing.T) {
	svc, _ := newHarness(t)
	ctx := context.Background()

	empty, err := svc.List(ctx, Filter{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if empty == nil || len(empty) != 0 {
		t.Fatalf("empty list must be non-nil []: %#v", empty)
	}

	mustCreate(t, svc, CreateRequest{RuleID: "b-global", Scope: ScopeGlobal, Type: RuleTypeRegime, Params: json.RawMessage(`{}`)})
	mustCreate(t, svc, CreateRequest{RuleID: "a-strategy", Scope: ScopeStrategy, Type: RuleTypeStopLoss, Params: json.RawMessage(`{}`)})
	if _, _, err := svc.Approve(ctx, ApproveRequest{RuleID: "a-strategy", Revision: 1}); err != nil {
		t.Fatalf("Approve: %v", err)
	}

	all, err := svc.List(ctx, Filter{})
	if err != nil {
		t.Fatalf("List all: %v", err)
	}
	if len(all) != 2 || all[0].RuleID != "a-strategy" || all[1].RuleID != "b-global" {
		t.Fatalf("list must be sorted by rule_id: %+v", all)
	}

	active, err := svc.List(ctx, Filter{Active: true})
	if err != nil {
		t.Fatalf("List active: %v", err)
	}
	if len(active) != 1 || active[0].RuleID != "a-strategy" {
		t.Fatalf("active filter must drop drafts: %+v", active)
	}

	scoped, err := svc.List(ctx, Filter{Scope: ScopeGlobal})
	if err != nil {
		t.Fatalf("List scope: %v", err)
	}
	if len(scoped) != 1 || scoped[0].RuleID != "b-global" {
		t.Fatalf("scope filter: %+v", scoped)
	}
}

func TestAudit_EveryWriteLeavesATrace(t *testing.T) {
	svc, _ := newHarness(t)
	ctx := context.Background()
	mustCreate(t, svc, CreateRequest{
		RuleID: "r", Scope: ScopeAccount, Type: RuleTypePositionSizing,
		Params: json.RawMessage(`{"pct":5}`), Actor: "alice",
	})
	if _, _, err := svc.Update(ctx, UpdateRequest{
		RuleID: "r", BaseRevision: 1, Params: json.RawMessage(`{"pct":6}`), Actor: "bob",
	}); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if _, _, err := svc.Approve(ctx, ApproveRequest{RuleID: "r", Revision: 1, Actor: "carol"}); err != nil {
		t.Fatalf("Approve 1: %v", err)
	}
	if _, _, err := svc.Approve(ctx, ApproveRequest{RuleID: "r", Revision: 2, Actor: "dave"}); err != nil {
		t.Fatalf("Approve 2: %v", err)
	}

	entries, err := svc.Audit(ctx, "r")
	if err != nil {
		t.Fatalf("Audit: %v", err)
	}
	want := []struct {
		action   string
		revision int
		actor    string
	}{
		{ActionCreate, 1, "alice"},
		{ActionUpdate, 2, "bob"},
		{ActionApprove, 1, "carol"},
		// 先记撤回、再记激活：审计顺序必须与状态迁移顺序一致。
		{ActionDeprecate, 1, "dave"},
		{ActionApprove, 2, "dave"},
	}
	if len(entries) != len(want) {
		t.Fatalf("want %d audit entries, got %d: %+v", len(want), len(entries), entries)
	}
	for i, w := range want {
		if entries[i].Action != w.action || entries[i].Revision != w.revision || entries[i].Actor != w.actor {
			t.Fatalf("audit[%d] = %+v, want %+v", i, entries[i], w)
		}
		if entries[i].Seq <= 0 {
			t.Fatalf("audit[%d] must carry a sequence, got %d", i, entries[i].Seq)
		}
		if len(entries[i].Detail) == 0 {
			t.Fatalf("audit[%d] must carry detail", i)
		}
	}
}

func TestDefinitionStatus_MatchesActivePointer(t *testing.T) {
	svc, _ := newHarness(t)
	ctx := context.Background()

	def, _, created, err := svc.Create(ctx, CreateRequest{
		RuleID: "r", Scope: ScopeGlobal, Type: RuleTypeRegime, Params: json.RawMessage(`{"w":20}`),
	})
	if err != nil || !created {
		t.Fatalf("Create: %v created=%v", err, created)
	}
	if def.Status != StatusDraft || def.ActiveRevision != 0 {
		t.Fatalf("new rule: want draft/no active, got %+v", def)
	}

	if _, _, err := svc.Approve(ctx, ApproveRequest{RuleID: "r", Revision: 1}); err != nil {
		t.Fatalf("Approve: %v", err)
	}

	// 产生新 draft 修订：定义仍是 active（有生效版本），但 head 是 draft。
	def, rev, err := svc.Update(ctx, UpdateRequest{RuleID: "r", BaseRevision: 1, Params: json.RawMessage(`{"w":30}`)})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if def.Status != StatusActive || def.ActiveRevision != 1 {
		t.Fatalf("definition status must track the active pointer, got %+v", def)
	}
	if rev.Status != StatusDraft {
		t.Fatalf("head revision must be draft, got %s", rev.Status)
	}
	views, err := svc.List(ctx, Filter{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(views) != 1 || views[0].Status != StatusDraft || views[0].ActiveRevision != 1 {
		t.Fatalf("view must expose head=draft with active=1: %+v", views)
	}
}

// ─── Memory 与 DDL ───────────────────────────────────────────────

func TestMemory_ReturnsDeepCopies(t *testing.T) {
	store := NewMemory()
	ctx := context.Background()
	def := Definition{RuleID: "r", Scope: ScopeGlobal, Type: RuleTypeRegime, HeadRevision: 1, Status: StatusDraft}
	rev := baseRevision("r")
	rev.VersionHash = "h"
	if err := store.CreateRule(ctx, def, rev); err != nil {
		t.Fatalf("CreateRule: %v", err)
	}

	got, err := store.GetRevision(ctx, "r", 1)
	if err != nil {
		t.Fatalf("GetRevision: %v", err)
	}
	got.Params[0] = 'X'
	again, err := store.GetRevision(ctx, "r", 1)
	if err != nil {
		t.Fatalf("GetRevision again: %v", err)
	}
	if string(again.Params) != `{"pct":2.5}` {
		t.Fatalf("store must not share Params memory, got %s", again.Params)
	}

	list, err := store.ListRevisions(ctx, "r")
	if err != nil {
		t.Fatalf("ListRevisions: %v", err)
	}
	list[0].Params[0] = 'Y'
	third, err := store.GetRevision(ctx, "r", 1)
	if err != nil {
		t.Fatalf("GetRevision third: %v", err)
	}
	if string(third.Params) != `{"pct":2.5}` {
		t.Fatalf("ListRevisions must return copies, got %s", third.Params)
	}

	if err := store.PutRevision(ctx, Revision{RuleID: "r", Revision: 1, VersionHash: "other"}); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("PutRevision with different hash: want ErrRevisionConflict, got %v", err)
	}
}

func TestMigrations_PinTheAdjudicatedShape(t *testing.T) {
	stmts := Migrations()
	if len(stmts) == 0 {
		t.Fatal("Migrations must not be empty")
	}
	for i, stmt := range stmts {
		if !strings.Contains(stmt, "IF NOT EXISTS") {
			t.Fatalf("statement %d is not re-entrant: %s", i, stmt)
		}
	}
	all := strings.Join(stmts, "\n")
	required := []string{
		"CREATE SCHEMA IF NOT EXISTS risk",
		"risk.rule_defs",
		"rule_id          TEXT PRIMARY KEY",
		"CHECK ((status = 'active') = (active_revision IS NOT NULL))",
		"risk.rule_revisions",
		"PRIMARY KEY (rule_id, revision)",
		"UNIQUE (rule_id, version_hash)",
		"CHECK ((status IN ('approved','active','deprecated')) = (approved_at IS NOT NULL))",
		"risk.audit_logs",
		"seq       BIGSERIAL PRIMARY KEY",
		"CHECK (action IN ('create','update','approve','deprecate'))",
	}
	for _, want := range required {
		if !strings.Contains(all, want) {
			t.Fatalf("DDL missing %q", want)
		}
	}

	// 调用方拿到的必须是副本，改它不能改到内部数组。
	stmts[0] = "DROP SCHEMA risk CASCADE"
	if Migrations()[0] == "DROP SCHEMA risk CASCADE" {
		t.Fatal("Migrations must return a copy")
	}
}

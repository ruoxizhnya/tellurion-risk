package rule

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

// TestMemoryTx_RollsBackAllWrites 钉住 Memory 的事务语义：fn 返回错误时，
// revision 状态、定义指针与审计都必须回到原样（与 PG 回滚一致）。
func TestMemoryTx_RollsBackAllWrites(t *testing.T) {
	svc, store := newHarness(t)
	ctx := context.Background()
	mustCreate(t, svc, CreateRequest{
		RuleID: "r", Scope: ScopeStrategy, Type: RuleTypeStopLoss, Params: json.RawMessage(`{"pct":2.5}`),
	})

	boom := errors.New("boom")
	err := store.Tx(ctx, func(st Store) error {
		def, err := st.GetDefinition(ctx, "r")
		if err != nil {
			return err
		}
		rev, err := st.GetRevision(ctx, "r", 1)
		if err != nil {
			return err
		}
		rev.Status = StatusApproved
		if err := st.PutRevision(ctx, rev); err != nil {
			return err
		}
		if _, err := st.AppendAudit(ctx, AuditEntry{RuleID: "r", Revision: 1, Action: ActionApprove}); err != nil {
			return err
		}
		def.Status = StatusActive
		def.ActiveRevision = 1
		if err := st.UpdateRule(ctx, def, rev); err != nil {
			return err
		}
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("Tx must surface the callback error, got %v", err)
	}

	rev, err := store.GetRevision(ctx, "r", 1)
	if err != nil {
		t.Fatalf("GetRevision: %v", err)
	}
	if rev.Status != StatusDraft || rev.ApprovedAt != nil {
		t.Fatalf("revision must roll back to draft, got %+v", rev)
	}
	def, err := store.GetDefinition(ctx, "r")
	if err != nil {
		t.Fatalf("GetDefinition: %v", err)
	}
	if def.Status != StatusDraft || def.ActiveRevision != 0 {
		t.Fatalf("definition must roll back, got %+v", def)
	}
	entries, err := store.ListAudit(ctx, "r")
	if err != nil {
		t.Fatalf("ListAudit: %v", err)
	}
	if len(entries) != 1 || entries[0].Action != ActionCreate {
		t.Fatalf("audit must roll back to the create row only, got %+v", entries)
	}
}

// failingApproveStore 在事务回调里注入第 N 次 PutRevision 失败，
// 用来证明 Approve 的读-改-写确实在 Tx 内、且失败会整笔回滚。
type failingApproveStore struct {
	*Memory
	failAt int
	puts   int
}

func (s *failingApproveStore) Tx(ctx context.Context, fn func(Store) error) error {
	return s.Memory.Tx(ctx, func(inner Store) error {
		return fn(&failingApproveTx{Store: inner, owner: s})
	})
}

type failingApproveTx struct {
	Store
	owner *failingApproveStore
}

func (t *failingApproveTx) PutRevision(ctx context.Context, rev Revision) error {
	t.owner.puts++
	if t.owner.puts == t.owner.failAt {
		return errors.New("injected put revision failure")
	}
	return t.Store.PutRevision(ctx, rev)
}

func TestServiceApprove_RollsBackAllWritesOnFailure(t *testing.T) {
	mem := NewMemory()
	store := &failingApproveStore{Memory: mem, failAt: 2}
	svc, err := New(Options{Store: store, Now: fixedNow})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx := context.Background()
	mustCreate(t, svc, CreateRequest{
		RuleID: "r", Scope: ScopeStrategy, Type: RuleTypeStopLoss, Params: json.RawMessage(`{"pct":2}`),
	})

	// 第 1 次 PutRevision（draft→approved）成功，第 2 次（→active）失败。
	if _, _, err := svc.Approve(ctx, ApproveRequest{RuleID: "r", Revision: 1}); err == nil {
		t.Fatal("Approve must fail when an intermediate write fails")
	}
	if store.puts != 2 {
		t.Fatalf("approve must run both PutRevision writes inside the transaction, got %d", store.puts)
	}

	rev, err := mem.GetRevision(ctx, "r", 1)
	if err != nil {
		t.Fatalf("GetRevision: %v", err)
	}
	if rev.Status != StatusDraft || rev.ApprovedAt != nil {
		t.Fatalf("failed approve must leave the revision draft, got %+v", rev)
	}
	def, err := mem.GetDefinition(ctx, "r")
	if err != nil {
		t.Fatalf("GetDefinition: %v", err)
	}
	if def.Status != StatusDraft || def.ActiveRevision != 0 {
		t.Fatalf("failed approve must not move the active pointer, got %+v", def)
	}
	entries, err := mem.ListAudit(ctx, "r")
	if err != nil {
		t.Fatalf("ListAudit: %v", err)
	}
	if len(entries) != 1 || entries[0].Action != ActionCreate {
		t.Fatalf("failed approve must not leave audit rows, got %+v", entries)
	}
}

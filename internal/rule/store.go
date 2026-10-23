package rule

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"sync"
)

// Filter 是 GET /api/risk/rules 的查询条件。
type Filter struct {
	Scope  Scope
	Active bool
}

// Store 是规则真相源的持久化端口。PG 实现见 internal/pgstore。
//
// 并发语义：Service 的写路径（create / update / approve / resolve-latest）都必须在
// Tx 内完成「读-改-写」；单靠进程内锁在多进程部署下会丢更新
// （docs/adr/0003-pg-store-与事务边界.md）。
type Store interface {
	GetDefinition(ctx context.Context, ruleID string) (Definition, error)
	ListDefinitions(ctx context.Context, f Filter) ([]Definition, error)
	CreateRule(ctx context.Context, def Definition, rev Revision) error
	UpdateRule(ctx context.Context, def Definition, rev Revision) error
	GetRevision(ctx context.Context, ruleID string, revision int) (Revision, error)
	ListRevisions(ctx context.Context, ruleID string) ([]Revision, error)
	PutRevision(ctx context.Context, rev Revision) error
	AppendAudit(ctx context.Context, entry AuditEntry) (int64, error)
	ListAudit(ctx context.Context, ruleID string) ([]AuditEntry, error)
	// PutSnapshot 落快照（内容寻址）：写入前重新 derive hash 校验；同 hash 重复写 = no-op，
	// 同 hash 不同内容 = ErrSnapshotMismatch（fail-loud，不许静默覆盖）。
	PutSnapshot(ctx context.Context, snap Snapshot) error
	// GetSnapshot 按 hash 取快照；读取时同样重新 derive 校验（两头都有牙）。
	GetSnapshot(ctx context.Context, hash string) (Snapshot, error)
	// Tx 在一个存储事务里执行 fn：事务内的读-改-写对其他事务不可见，
	// fn 返回错误时整笔回滚。Memory 与 PG 必须给出相同语义。
	Tx(ctx context.Context, fn func(Store) error) error
}

// Memory 是进程内 Store。低频率的管理操作；不作为生产持久化。
// Tx 会先把全部状态做一份深拷贝，失败时还原（回滚语义与 PG 一致）。
type Memory struct {
	mu          sync.RWMutex
	defs        map[string]Definition
	revisions   map[string]map[int]Revision
	audit       []AuditEntry
	auditSeq    int64
	auditByRule map[string][]int64
	snapshots   map[string]Snapshot
}

// NewMemory 构造空的内存 Store。
func NewMemory() *Memory {
	return &Memory{
		defs:        make(map[string]Definition),
		revisions:   make(map[string]map[int]Revision),
		auditByRule: make(map[string][]int64),
		snapshots:   make(map[string]Snapshot),
	}
}

// memoryState 是 Tx 的回滚快照。所有容器都深拷贝，回调里替换值不会污染备份。
type memoryState struct {
	defs        map[string]Definition
	revisions   map[string]map[int]Revision
	audit       []AuditEntry
	auditSeq    int64
	auditByRule map[string][]int64
	snapshots   map[string]Snapshot
}

func (m *Memory) snapshotState() memoryState {
	st := memoryState{
		defs:        make(map[string]Definition, len(m.defs)),
		revisions:   make(map[string]map[int]Revision, len(m.revisions)),
		audit:       make([]AuditEntry, len(m.audit)),
		auditSeq:    m.auditSeq,
		auditByRule: make(map[string][]int64, len(m.auditByRule)),
		snapshots:   make(map[string]Snapshot, len(m.snapshots)),
	}
	for k, v := range m.defs {
		st.defs[k] = v
	}
	for k, revs := range m.revisions {
		cp := make(map[int]Revision, len(revs))
		for rk, rv := range revs {
			cp[rk] = rv.Clone()
		}
		st.revisions[k] = cp
	}
	for i, entry := range m.audit {
		entry.Detail = append(json.RawMessage(nil), entry.Detail...)
		st.audit[i] = entry
	}
	for k, idxs := range m.auditByRule {
		st.auditByRule[k] = append([]int64(nil), idxs...)
	}
	for k, snap := range m.snapshots {
		st.snapshots[k] = snap.Clone()
	}
	return st
}

func (m *Memory) restoreState(st memoryState) {
	m.defs = st.defs
	m.revisions = st.revisions
	m.audit = st.audit
	m.auditSeq = st.auditSeq
	m.auditByRule = st.auditByRule
	m.snapshots = st.snapshots
}

// Tx 串行化全部写操作；fn 失败则整笔还原。
func (m *Memory) Tx(_ context.Context, fn func(Store) error) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	backup := m.snapshotState()
	if err := fn(&memoryTx{m: m}); err != nil {
		m.restoreState(backup)
		return err
	}
	return nil
}

// memoryTx 是 Tx 回调里的 Store 视图：外层已持写锁，方法不再加锁。
type memoryTx struct{ m *Memory }

func (t *memoryTx) Tx(_ context.Context, fn func(Store) error) error { return fn(t) }

func (t *memoryTx) GetDefinition(ctx context.Context, ruleID string) (Definition, error) {
	return t.m.getDefinition(ctx, ruleID)
}

func (t *memoryTx) ListDefinitions(ctx context.Context, f Filter) ([]Definition, error) {
	return t.m.listDefinitions(ctx, f)
}

func (t *memoryTx) CreateRule(ctx context.Context, def Definition, rev Revision) error {
	return t.m.createRule(ctx, def, rev)
}

func (t *memoryTx) UpdateRule(ctx context.Context, def Definition, rev Revision) error {
	return t.m.updateRule(ctx, def, rev)
}

func (t *memoryTx) GetRevision(ctx context.Context, ruleID string, revision int) (Revision, error) {
	return t.m.getRevision(ctx, ruleID, revision)
}

func (t *memoryTx) ListRevisions(ctx context.Context, ruleID string) ([]Revision, error) {
	return t.m.listRevisions(ctx, ruleID)
}

func (t *memoryTx) PutRevision(ctx context.Context, rev Revision) error {
	return t.m.putRevision(ctx, rev)
}

func (t *memoryTx) AppendAudit(ctx context.Context, entry AuditEntry) (int64, error) {
	return t.m.appendAudit(ctx, entry)
}

func (t *memoryTx) ListAudit(ctx context.Context, ruleID string) ([]AuditEntry, error) {
	return t.m.listAudit(ctx, ruleID)
}

func (t *memoryTx) PutSnapshot(ctx context.Context, snap Snapshot) error {
	return t.m.putSnapshot(ctx, snap)
}

func (t *memoryTx) GetSnapshot(ctx context.Context, hash string) (Snapshot, error) {
	return t.m.getSnapshot(ctx, hash)
}

func (m *Memory) getDefinition(_ context.Context, ruleID string) (Definition, error) {
	def, ok := m.defs[ruleID]
	if !ok {
		return Definition{}, ErrRuleNotFound
	}
	return def, nil
}

func (m *Memory) listDefinitions(_ context.Context, f Filter) ([]Definition, error) {
	out := make([]Definition, 0, len(m.defs))
	for _, def := range m.defs {
		if f.Scope != "" && def.Scope != f.Scope {
			continue
		}
		if f.Active && def.Status != StatusActive {
			continue
		}
		out = append(out, def)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].RuleID < out[j].RuleID })
	return out, nil
}

func (m *Memory) createRule(_ context.Context, def Definition, rev Revision) error {
	if _, ok := m.defs[def.RuleID]; ok {
		return ErrIdempotencyConflict
	}
	m.defs[def.RuleID] = def
	m.revisions[def.RuleID] = map[int]Revision{rev.Revision: rev.Clone()}
	return nil
}

func (m *Memory) updateRule(_ context.Context, def Definition, rev Revision) error {
	if _, ok := m.defs[def.RuleID]; !ok {
		return ErrRuleNotFound
	}
	m.defs[def.RuleID] = def
	revs, ok := m.revisions[def.RuleID]
	if !ok {
		revs = make(map[int]Revision)
		m.revisions[def.RuleID] = revs
	}
	revs[rev.Revision] = rev.Clone()
	return nil
}

func (m *Memory) getRevision(_ context.Context, ruleID string, revision int) (Revision, error) {
	revs, ok := m.revisions[ruleID]
	if !ok {
		return Revision{}, ErrRuleNotFound
	}
	rev, ok := revs[revision]
	if !ok {
		return Revision{}, ErrRevisionNotFound
	}
	return rev.Clone(), nil
}

func (m *Memory) listRevisions(_ context.Context, ruleID string) ([]Revision, error) {
	revs, ok := m.revisions[ruleID]
	if !ok {
		return nil, ErrRuleNotFound
	}
	out := make([]Revision, 0, len(revs))
	for _, rev := range revs {
		out = append(out, rev.Clone())
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Revision < out[j].Revision })
	return out, nil
}

func (m *Memory) putRevision(_ context.Context, rev Revision) error {
	revs, ok := m.revisions[rev.RuleID]
	if !ok {
		return ErrRuleNotFound
	}
	existing, ok := revs[rev.Revision]
	if !ok {
		return ErrRevisionNotFound
	}
	if existing.VersionHash != rev.VersionHash {
		return ErrRevisionConflict
	}
	revs[rev.Revision] = rev.Clone()
	return nil
}

func (m *Memory) appendAudit(_ context.Context, entry AuditEntry) (int64, error) {
	m.auditSeq++
	entry.Seq = m.auditSeq
	m.audit = append(m.audit, entry)
	m.auditByRule[entry.RuleID] = append(m.auditByRule[entry.RuleID], int64(len(m.audit)-1))
	return entry.Seq, nil
}

func (m *Memory) listAudit(_ context.Context, ruleID string) ([]AuditEntry, error) {
	idxs := m.auditByRule[ruleID]
	out := make([]AuditEntry, 0, len(idxs))
	for _, i := range idxs {
		entry := m.audit[i]
		entry.Detail = append(json.RawMessage(nil), entry.Detail...)
		out = append(out, entry)
	}
	return out, nil
}

func (m *Memory) putSnapshot(_ context.Context, snap Snapshot) error {
	if err := verifyHash(snap.RiskRulesHash, snap.Rules); err != nil {
		return err
	}
	if existing, ok := m.snapshots[snap.RiskRulesHash]; ok {
		if !sameSnapshotRules(existing.Rules, snap.Rules) {
			return fmt.Errorf("%w: hash %s already stored with different content",
				ErrSnapshotMismatch, snap.RiskRulesHash)
		}
		return nil
	}
	m.snapshots[snap.RiskRulesHash] = snap.Clone()
	return nil
}

func (m *Memory) getSnapshot(_ context.Context, hash string) (Snapshot, error) {
	snap, ok := m.snapshots[hash]
	if !ok {
		return Snapshot{}, ErrSnapshotNotFound
	}
	if err := verifyHash(snap.RiskRulesHash, snap.Rules); err != nil {
		return Snapshot{}, err
	}
	return snap.Clone(), nil
}

// GetDefinition 返回定义的深拷贝视图。
func (m *Memory) GetDefinition(ctx context.Context, ruleID string) (Definition, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.getDefinition(ctx, ruleID)
}

// ListDefinitions 按 scope / active 过滤，按 rule_id 排序（顺序稳定可测）。
func (m *Memory) ListDefinitions(ctx context.Context, f Filter) ([]Definition, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.listDefinitions(ctx, f)
}

// CreateRule 落定义与首个 revision；已存在则不得覆盖。
func (m *Memory) CreateRule(ctx context.Context, def Definition, rev Revision) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.createRule(ctx, def, rev)
}

// UpdateRule 落新 revision 并更新定义。
func (m *Memory) UpdateRule(ctx context.Context, def Definition, rev Revision) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.updateRule(ctx, def, rev)
}

// GetRevision 返回 revision 深拷贝。
func (m *Memory) GetRevision(ctx context.Context, ruleID string, revision int) (Revision, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.getRevision(ctx, ruleID, revision)
}

// ListRevisions 按 revision 升序返回深拷贝。
func (m *Memory) ListRevisions(ctx context.Context, ruleID string) ([]Revision, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.listRevisions(ctx, ruleID)
}

// PutRevision 覆盖写单个 revision 的状态（审批跃迁用），内容字段必须与已存一致。
func (m *Memory) PutRevision(ctx context.Context, rev Revision) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.putRevision(ctx, rev)
}

// AppendAudit 追加审计并回填序号。
func (m *Memory) AppendAudit(ctx context.Context, entry AuditEntry) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.appendAudit(ctx, entry)
}

// ListAudit 按写入顺序返回某条规则的审计。
func (m *Memory) ListAudit(ctx context.Context, ruleID string) ([]AuditEntry, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.listAudit(ctx, ruleID)
}

// PutSnapshot 落快照：先重 derive 校验，再按 hash 幂等写（同内容 no-op，同 hash 异内容 fail-loud）。
func (m *Memory) PutSnapshot(ctx context.Context, snap Snapshot) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.putSnapshot(ctx, snap)
}

// GetSnapshot 按 hash 取快照，读取时重新 derive 校验。
func (m *Memory) GetSnapshot(ctx context.Context, hash string) (Snapshot, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.getSnapshot(ctx, hash)
}

// sameSnapshotRules 逐条比对规则内容（params 走 canonical，键序 / 空白不参与）。
func sameSnapshotRules(a, b []SnapshotRule) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].RuleID != b[i].RuleID || a[i].Revision != b[i].Revision ||
			a[i].Scope != b[i].Scope || a[i].Type != b[i].Type || a[i].VersionHash != b[i].VersionHash {
			return false
		}
		pa, err := canonicalParams(a[i].Params)
		if err != nil {
			return false
		}
		pb, err := canonicalParams(b[i].Params)
		if err != nil {
			return false
		}
		if !bytes.Equal(pa, pb) {
			return false
		}
	}
	return true
}

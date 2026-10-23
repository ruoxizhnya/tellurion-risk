package rule

import (
	"context"
	"encoding/json"
	"sort"
	"sync"
)

// Filter 是 GET /api/risk/rules 的查询条件。
type Filter struct {
	Scope  Scope
	Active bool
}

// Store 是规则真相源的持久化端口。PG 实现未落（docs/TASKS.md QT-42 施工记录）；
// 端口语义已按「读-改-写必须由 Service 串行」设计，PG 实现需用事务承接。
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
}

// Memory 是进程内 Store。低频率的管理操作，单锁足够；不作为生产持久化。
type Memory struct {
	mu          sync.RWMutex
	defs        map[string]Definition
	revisions   map[string]map[int]Revision
	audit       []AuditEntry
	auditSeq    int64
	auditByRule map[string][]int64
}

// NewMemory 构造空的内存 Store。
func NewMemory() *Memory {
	return &Memory{
		defs:        make(map[string]Definition),
		revisions:   make(map[string]map[int]Revision),
		auditByRule: make(map[string][]int64),
	}
}

// GetDefinition 返回定义的深拷贝视图。
func (m *Memory) GetDefinition(_ context.Context, ruleID string) (Definition, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	def, ok := m.defs[ruleID]
	if !ok {
		return Definition{}, ErrRuleNotFound
	}
	return def, nil
}

// ListDefinitions 按 scope / active 过滤，按 rule_id 排序（顺序稳定可测）。
func (m *Memory) ListDefinitions(_ context.Context, f Filter) ([]Definition, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
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

// CreateRule 落定义与首个 revision；已存在则 ErrRuleNotFound 之外不得覆盖。
func (m *Memory) CreateRule(_ context.Context, def Definition, rev Revision) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.defs[def.RuleID]; ok {
		return ErrIdempotencyConflict
	}
	m.defs[def.RuleID] = def
	m.revisions[def.RuleID] = map[int]Revision{rev.Revision: rev.Clone()}
	return nil
}

// UpdateRule 落新 revision 并更新定义。
func (m *Memory) UpdateRule(_ context.Context, def Definition, rev Revision) error {
	m.mu.Lock()
	defer m.mu.Unlock()
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

// GetRevision 返回 revision 深拷贝。
func (m *Memory) GetRevision(_ context.Context, ruleID string, revision int) (Revision, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
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

// ListRevisions 按 revision 升序返回深拷贝。
func (m *Memory) ListRevisions(_ context.Context, ruleID string) ([]Revision, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
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

// PutRevision 覆盖写单个 revision 的状态（审批跃迁用），内容字段必须与已存一致。
func (m *Memory) PutRevision(_ context.Context, rev Revision) error {
	m.mu.Lock()
	defer m.mu.Unlock()
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

// AppendAudit 追加审计并回填序号。
func (m *Memory) AppendAudit(_ context.Context, entry AuditEntry) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.auditSeq++
	entry.Seq = m.auditSeq
	m.audit = append(m.audit, entry)
	m.auditByRule[entry.RuleID] = append(m.auditByRule[entry.RuleID], int64(len(m.audit)-1))
	return entry.Seq, nil
}

// ListAudit 按写入顺序返回某条规则的审计。
func (m *Memory) ListAudit(_ context.Context, ruleID string) ([]AuditEntry, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	idxs := m.auditByRule[ruleID]
	out := make([]AuditEntry, 0, len(idxs))
	for _, i := range idxs {
		entry := m.audit[i]
		entry.Detail = append(json.RawMessage(nil), entry.Detail...)
		out = append(out, entry)
	}
	return out, nil
}

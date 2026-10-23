package rule

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"
)

// Options 构造 Service。
type Options struct {
	Store Store
	// Now 可注入（测试固定时间）；零值取 time.Now。
	Now func() time.Time
}

// Service 是规则真相源的领域门面：定义 / 版本 / 审批 / 审计。
//
// 并发约定：读-改-写必须原子，所以写操作由本结构串行。PG Store 接管时这把锁要换成事务。
type Service struct {
	store Store
	now   func() time.Time
	mu    sync.Mutex
}

// New 构造 Service。
func New(opts Options) (*Service, error) {
	if opts.Store == nil {
		return nil, errors.New("rule: store is required")
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	return &Service{store: opts.Store, now: opts.Now}, nil
}

// CreateRequest 建规则（draft）。
type CreateRequest struct {
	RuleID      string          `json:"rule_id"`
	Scope       Scope           `json:"scope"`
	Type        RuleType        `json:"type"`
	Params      json.RawMessage `json:"params"`
	Description string          `json:"description,omitempty"`
	// Actor 不从 HTTP 体读取（`json:"-"`）：鉴权面未落（QT-50 ④），调用方不能自报身份。
	// Service 内部/将来的鉴权中间件可以直接赋值。
	Actor string `json:"-"`
}

// UpdateRequest 改规则 → 生成新 revision。
type UpdateRequest struct {
	RuleID       string          `json:"rule_id"`
	BaseRevision int             `json:"base_revision"`
	Params       json.RawMessage `json:"params"`
	// Description 为 nil 表示保持原描述（与"传空串清空"区分）。
	Description *string `json:"description,omitempty"`
	Actor       string  `json:"-"`
}

// ApproveRequest 审批并激活某个 revision。
type ApproveRequest struct {
	RuleID   string `json:"rule_id"`
	Revision int    `json:"revision"`
	Actor    string `json:"-"`
}

// RuleView 是 GET /rules 的列表项形状：head revision 的内容身份 + 规则是否有生效版本。
// Status 是 **head revision** 的状态（新建修订后 head 是 draft，即使上一条仍是 active）；
// ActiveRevision 指向当前生效的 revision（0 = 从未激活）。
type RuleView struct {
	RuleID         string   `json:"rule_id"`
	Revision       int      `json:"revision"`
	Status         Status   `json:"status"`
	VersionHash    string   `json:"version_hash"`
	Scope          Scope    `json:"scope"`
	Type           RuleType `json:"type"`
	Description    string   `json:"description,omitempty"`
	ActiveRevision int      `json:"active_revision,omitempty"`
}

// Create 建规则。幂等：同 rule_id 同内容 → 返回既有 head（created=false，HTTP 200）；
// 同 rule_id 不同内容 → ErrIdempotencyConflict（HTTP 409）。
func (s *Service) Create(ctx context.Context, req CreateRequest) (Definition, Revision, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	rev := Revision{
		RuleID:      req.RuleID,
		Revision:    1,
		Scope:       req.Scope,
		Type:        req.Type,
		Params:      append(json.RawMessage(nil), req.Params...),
		Description: req.Description,
		Status:      StatusDraft,
	}
	if err := rev.Validate(); err != nil {
		return Definition{}, Revision{}, false, err
	}
	hash, err := VersionHash(rev)
	if err != nil {
		return Definition{}, Revision{}, false, err
	}
	rev.VersionHash = hash
	now := s.now().UTC()
	rev.CreatedAt = now

	if existing, err := s.store.GetDefinition(ctx, req.RuleID); err == nil {
		head, err := s.store.GetRevision(ctx, req.RuleID, existing.HeadRevision)
		if err != nil {
			return Definition{}, Revision{}, false, err
		}
		if !sameContent(head, rev) {
			return Definition{}, Revision{}, false, fmt.Errorf("%w: %s", ErrIdempotencyConflict, req.RuleID)
		}
		return existing, head, false, nil
	} else if !errors.Is(err, ErrRuleNotFound) {
		return Definition{}, Revision{}, false, err
	}

	def := Definition{
		RuleID:       req.RuleID,
		Scope:        req.Scope,
		Type:         req.Type,
		Description:  req.Description,
		HeadRevision: 1,
		Status:       StatusDraft,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if err := s.store.CreateRule(ctx, def, rev); err != nil {
		return Definition{}, Revision{}, false, err
	}
	if err := s.audit(ctx, req.RuleID, rev.Revision, ActionCreate, req.Actor, map[string]any{
		"scope": string(rev.Scope),
		"type":  string(rev.Type),
	}); err != nil {
		return Definition{}, Revision{}, false, err
	}
	return def, rev.Clone(), true, nil
}

// Update 生成新 revision。base_revision 必须等于当前 head（防丢更新）。
func (s *Service) Update(ctx context.Context, req UpdateRequest) (Definition, Revision, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	def, err := s.store.GetDefinition(ctx, req.RuleID)
	if err != nil {
		return Definition{}, Revision{}, err
	}
	if def.HeadRevision != req.BaseRevision {
		return Definition{}, Revision{}, fmt.Errorf("%w: base %d, head %d",
			ErrRevisionConflict, req.BaseRevision, def.HeadRevision)
	}
	head, err := s.store.GetRevision(ctx, req.RuleID, def.HeadRevision)
	if err != nil {
		return Definition{}, Revision{}, err
	}
	description := head.Description
	if req.Description != nil {
		description = *req.Description
	}
	rev := Revision{
		RuleID:      req.RuleID,
		Revision:    head.Revision + 1,
		Scope:       head.Scope,
		Type:        head.Type,
		Params:      append(json.RawMessage(nil), req.Params...),
		Description: description,
		Status:      StatusDraft,
	}
	if err := rev.Validate(); err != nil {
		return Definition{}, Revision{}, err
	}
	hash, err := VersionHash(rev)
	if err != nil {
		return Definition{}, Revision{}, err
	}
	rev.VersionHash = hash
	now := s.now().UTC()
	rev.CreatedAt = now

	def.HeadRevision = rev.Revision
	// 定义级 status 只表达「当前有没有生效的 revision」：有 active 就保持 active，
	// 否则 draft。head 自己是不是 draft 由 RuleView.Status 表达（见 List）。
	// 这样 DDL 的 CHECK((status='active')=(active_revision IS NOT NULL)) 恒成立。
	if def.ActiveRevision == 0 {
		def.Status = StatusDraft
	}
	def.Description = description
	def.UpdatedAt = now
	if err := s.store.UpdateRule(ctx, def, rev); err != nil {
		return Definition{}, Revision{}, err
	}
	if err := s.audit(ctx, req.RuleID, rev.Revision, ActionUpdate, req.Actor, map[string]any{
		"base_revision": req.BaseRevision,
	}); err != nil {
		return Definition{}, Revision{}, err
	}
	return def, rev.Clone(), nil
}

// Approve 把 draft revision 激活：draft → approved → active。
// 同规则上一条 active revision 跃迁为 deprecated（撤回不是删除，历史快照仍可按 hash 复现）。
// 对同一 revision 重放是幂等的：返回同一 version_hash。
func (s *Service) Approve(ctx context.Context, req ApproveRequest) (Definition, Revision, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	def, err := s.store.GetDefinition(ctx, req.RuleID)
	if err != nil {
		return Definition{}, Revision{}, err
	}
	rev, err := s.store.GetRevision(ctx, req.RuleID, req.Revision)
	if err != nil {
		return Definition{}, Revision{}, err
	}
	switch rev.Status {
	case StatusActive:
		return def, rev, nil
	case StatusApproved:
		// 上一次调用在 approved 之后中断：继续走完到 active（同一路径，不另开第二条路）。
	case StatusDraft:
		now := s.now().UTC()
		rev.Status = StatusApproved
		rev.ApprovedAt = &now
		if err := s.store.PutRevision(ctx, rev); err != nil {
			return Definition{}, Revision{}, err
		}
	case StatusDeprecated:
		return Definition{}, Revision{}, fmt.Errorf("%w: %s@%d is deprecated", ErrRevisionNotDraft, req.RuleID, req.Revision)
	default:
		return Definition{}, Revision{}, fmt.Errorf("%w: %s", ErrRevisionNotDraft, rev.Status)
	}

	if def.ActiveRevision > 0 && def.ActiveRevision != rev.Revision {
		prev, err := s.store.GetRevision(ctx, req.RuleID, def.ActiveRevision)
		if err != nil {
			return Definition{}, Revision{}, err
		}
		if prev.Status == StatusActive {
			prev.Status = StatusDeprecated
			if err := s.store.PutRevision(ctx, prev); err != nil {
				return Definition{}, Revision{}, err
			}
			if err := s.audit(ctx, req.RuleID, prev.Revision, ActionDeprecate, req.Actor, map[string]any{
				"superseded_by": rev.Revision,
			}); err != nil {
				return Definition{}, Revision{}, err
			}
		}
	}

	now := s.now().UTC()
	rev.Status = StatusActive
	rev.ApprovedAt = &now
	if err := s.store.PutRevision(ctx, rev); err != nil {
		return Definition{}, Revision{}, err
	}

	def.HeadRevision = rev.Revision
	def.Status = StatusActive
	def.ActiveRevision = rev.Revision
	def.UpdatedAt = now
	if err := s.store.UpdateRule(ctx, def, rev); err != nil {
		return Definition{}, Revision{}, err
	}
	if err := s.audit(ctx, req.RuleID, rev.Revision, ActionApprove, req.Actor, map[string]any{
		"version_hash": rev.VersionHash,
	}); err != nil {
		return Definition{}, Revision{}, err
	}
	return def, rev.Clone(), nil
}

// List 返回规则列表（空列表是 []，不是 nil）。
func (s *Service) List(ctx context.Context, f Filter) ([]RuleView, error) {
	defs, err := s.store.ListDefinitions(ctx, f)
	if err != nil {
		return nil, err
	}
	out := make([]RuleView, 0, len(defs))
	for _, def := range defs {
		head, err := s.store.GetRevision(ctx, def.RuleID, def.HeadRevision)
		if err != nil {
			return nil, err
		}
		out = append(out, RuleView{
			RuleID:         def.RuleID,
			Revision:       def.HeadRevision,
			Status:         head.Status,
			VersionHash:    head.VersionHash,
			Scope:          def.Scope,
			Type:           def.Type,
			Description:    def.Description,
			ActiveRevision: def.ActiveRevision,
		})
	}
	return out, nil
}

// Audit 返回某条规则的审计（写操作必留痕；actor 待 QT-50 ④ 鉴权面落地后才有值）。
func (s *Service) Audit(ctx context.Context, ruleID string) ([]AuditEntry, error) {
	return s.store.ListAudit(ctx, ruleID)
}

func (s *Service) audit(ctx context.Context, ruleID string, revision int, action, actor string, detail any) error {
	raw, err := json.Marshal(detail)
	if err != nil {
		return fmt.Errorf("rule: audit detail: %w", err)
	}
	_, err = s.store.AppendAudit(ctx, AuditEntry{
		RuleID:   ruleID,
		Revision: revision,
		Action:   action,
		Actor:    actor,
		Detail:   raw,
		At:       s.now().UTC(),
	})
	return err
}

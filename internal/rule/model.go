package rule

import (
	"bytes"
	"encoding/json"
	"fmt"
	"time"
)

// RuleType 是冻结的规则类型枚举。旧读者遇到未知类型必须 fail-closed 拒收
// （docs/ARCHITECTURE.md §9.6）；新增类型 = MINOR，改既有语义 = breaking。
type RuleType string

const (
	RuleTypePositionSizing RuleType = "position_sizing"
	RuleTypeStopLoss       RuleType = "stop_loss"
	RuleTypeRegime         RuleType = "regime"
	RuleTypeTakeProfit     RuleType = "take_profit"
)

var ruleTypes = map[RuleType]struct{}{
	RuleTypePositionSizing: {},
	RuleTypeStopLoss:       {},
	RuleTypeRegime:         {},
	RuleTypeTakeProfit:     {},
}

// Scope 是规则适用范围。找不到对应 scope 的规则 = 没有规则，不允许"用默认限额"。
type Scope string

const (
	ScopeGlobal   Scope = "global"
	ScopeAccount  Scope = "account"
	ScopeStrategy Scope = "strategy"
	ScopeSymbol   Scope = "symbol"
)

var scopes = map[Scope]struct{}{
	ScopeGlobal:   {},
	ScopeAccount:  {},
	ScopeStrategy: {},
	ScopeSymbol:   {},
}

// Valid 报告 scope 是否在冻结枚举里。跨包（熔断指令等）复用同一份枚举，
// 不在别处再抄一遍字符串。
func (s Scope) Valid() bool {
	_, ok := scopes[s]
	return ok
}

// Status 是 revision 的审批状态。状态机之外没有第二条路：
// draft → approved → active → deprecated（`active → deprecated` 只在同规则新 revision 激活时发生）。
type Status string

const (
	StatusDraft      Status = "draft"
	StatusApproved   Status = "approved"
	StatusActive     Status = "active"
	StatusDeprecated Status = "deprecated"
)

// MaxParamsBytes 是可接受参数的体积上限；超限 fail-loud，不截断。
const MaxParamsBytes = 64 << 10

// Definition 是规则定义的当前视图（head 指向某一 revision）。
type Definition struct {
	RuleID         string    `json:"rule_id"`
	Scope          Scope     `json:"scope"`
	Type           RuleType  `json:"type"`
	Description    string    `json:"description,omitempty"`
	HeadRevision   int       `json:"revision"`
	Status         Status    `json:"status"`
	ActiveRevision int       `json:"active_revision,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// Revision 是一次不可变的规则内容快照。version_hash 由内容派生，内容改必须改 hash。
type Revision struct {
	RuleID      string          `json:"rule_id"`
	Revision    int             `json:"revision"`
	Scope       Scope           `json:"scope"`
	Type        RuleType        `json:"type"`
	Params      json.RawMessage `json:"params"`
	Description string          `json:"description,omitempty"`
	Status      Status          `json:"status"`
	VersionHash string          `json:"version_hash"`
	CreatedAt   time.Time       `json:"created_at"`
	ApprovedAt  *time.Time      `json:"approved_at,omitempty"`
}

// Clone 返回深拷贝（Params 与 ApprovedAt 都拷），避免调用方与 Store 共享可变内存。
func (r Revision) Clone() Revision {
	out := r
	out.Params = append(json.RawMessage(nil), r.Params...)
	if r.ApprovedAt != nil {
		at := *r.ApprovedAt
		out.ApprovedAt = &at
	}
	return out
}

// AuditEntry 是人操作的留痕（规则变更 / 审批）。与 core 的鉴权访问审计不是一回事，
// 不复用 core 的 audit_logs（docs/db-schema.md §1 注）。
type AuditEntry struct {
	Seq      int64           `json:"seq"`
	RuleID   string          `json:"rule_id"`
	Revision int             `json:"revision"`
	Action   string          `json:"action"`
	Actor    string          `json:"actor,omitempty"`
	Detail   json.RawMessage `json:"detail,omitempty"`
	At       time.Time       `json:"at"`
}

// Action 取值；审计里同一动作字符串是查询口径，改名 = breaking。
const (
	ActionCreate    = "create"
	ActionUpdate    = "update"
	ActionApprove   = "approve"
	ActionDeprecate = "deprecate"
)

// Validate 校验 revision 的内容。未知类型 / scope / 非对象参数一律拒绝。
func (r Revision) Validate() error {
	if r.RuleID == "" {
		return fmt.Errorf("%w: rule_id is required", ErrInvalidParams)
	}
	if _, ok := ruleTypes[r.Type]; !ok {
		return fmt.Errorf("%w: %q", ErrInvalidRuleType, r.Type)
	}
	if _, ok := scopes[r.Scope]; !ok {
		return fmt.Errorf("%w: %q", ErrInvalidScope, r.Scope)
	}
	if r.Revision <= 0 {
		return fmt.Errorf("%w: revision must be > 0", ErrInvalidParams)
	}
	params, err := canonicalParams(r.Params)
	if err != nil {
		return err
	}
	if len(params) > MaxParamsBytes {
		return fmt.Errorf("%w: params exceed %d bytes", ErrInvalidParams, MaxParamsBytes)
	}
	return nil
}

// canonicalParams 把参数规范化成「键有序、无多余空白」的 JSON 对象字节。
// 规范化是 version_hash 可复现的前提：同一对象的不同书写必须同 hash。
func canonicalParams(raw json.RawMessage) ([]byte, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil, fmt.Errorf("%w: params is required", ErrInvalidParams)
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, fmt.Errorf("%w: params is not valid JSON: %v", ErrInvalidParams, err)
	}
	if dec.More() {
		return nil, fmt.Errorf("%w: params has trailing content", ErrInvalidParams)
	}
	if _, ok := v.(map[string]any); !ok {
		return nil, fmt.Errorf("%w: params must be a JSON object", ErrInvalidParams)
	}
	out, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("%w: params cannot be canonicalised: %v", ErrInvalidParams, err)
	}
	if len(out) > MaxParamsBytes {
		return nil, fmt.Errorf("%w: params exceed %d bytes", ErrInvalidParams, MaxParamsBytes)
	}
	return out, nil
}

func sameContent(a, b Revision) bool {
	if a.Scope != b.Scope || a.Type != b.Type || a.Description != b.Description {
		return false
	}
	pa, err := canonicalParams(a.Params)
	if err != nil {
		return false
	}
	pb, err := canonicalParams(b.Params)
	if err != nil {
		return false
	}
	return bytes.Equal(pa, pb)
}

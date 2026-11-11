package rule

import (
	"bytes"
	"encoding/json"
	"fmt"
	"time"

	contractsrisk "github.com/ruoxizhnya/tellurion-contracts/risk"
)

// RuleType / Scope 与两组冻结词表由 `contracts/risk` 唯一持有（ADR-040）。
//
// 本仓只做类型别名：JSON 词表、`errors.Is` 判定与契约侧逐字相同；新增词表走
// contracts 发版，不在本仓另抄一份。`Scope.Valid()` 也由契约提供（纯函数，
// 熔断指令等跨包复用同一份枚举）。
type RuleType = contractsrisk.RuleType

const (
	RuleTypePositionSizing = contractsrisk.RuleTypePositionSizing
	RuleTypeStopLoss       = contractsrisk.RuleTypeStopLoss
	RuleTypeRegime         = contractsrisk.RuleTypeRegime
	RuleTypeTakeProfit     = contractsrisk.RuleTypeTakeProfit
)

var ruleTypes = map[RuleType]struct{}{
	RuleTypePositionSizing: {},
	RuleTypeStopLoss:       {},
	RuleTypeRegime:         {},
	RuleTypeTakeProfit:     {},
}

// Scope 是规则适用范围。找不到对应 scope 的规则 = 没有规则，不允许"用默认限额"。
type Scope = contractsrisk.Scope

const (
	ScopeGlobal   = contractsrisk.ScopeGlobal
	ScopeAccount  = contractsrisk.ScopeAccount
	ScopeStrategy = contractsrisk.ScopeStrategy
	ScopeSymbol   = contractsrisk.ScopeSymbol
)

var scopes = map[Scope]struct{}{
	ScopeGlobal:   {},
	ScopeAccount:  {},
	ScopeStrategy: {},
	ScopeSymbol:   {},
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

// Validate 校验 revision 的内容。未知类型 / scope / 非对象参数一律拒绝；
// params 的键集合与取值按 RuleType 对应的参数字典逐项校验（params.go）。
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
	// 写侧严格：未知键 / 缺必填 / 类型不符 / NaN / Inf / 越域 / 跨字段矛盾都拒。
	return validateParams(r.Type, params)
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

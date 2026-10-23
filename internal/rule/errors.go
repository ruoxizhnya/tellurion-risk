package rule

import "errors"

// 领域哨兵。wire code 与它们的映射见 internal/httpapi 与 docs/api-schema.md §2。
var (
	// ErrRuleNotFound 规则不存在。
	ErrRuleNotFound = errors.New("rule not found")
	// ErrRevisionNotFound revision 不存在。
	ErrRevisionNotFound = errors.New("rule revision not found")
	// ErrRevisionConflict base_revision 不是当前 head（防丢更新）。
	ErrRevisionConflict = errors.New("base revision is not the current head")
	// ErrRevisionNotDraft 审批的 revision 不是 draft。
	ErrRevisionNotDraft = errors.New("revision is not in draft status")
	// ErrIdempotencyConflict 同一 rule_id 不同内容。
	ErrIdempotencyConflict = errors.New("rule_id already exists with different content")
	// ErrInvalidRuleType 规则类型不在冻结枚举。
	ErrInvalidRuleType = errors.New("rule type is not in the frozen enum")
	// ErrInvalidScope 适用范围不在冻结枚举。
	ErrInvalidScope = errors.New("scope is not in the frozen enum")
	// ErrInvalidParams 参数不是合法 JSON 对象、为空或超限。
	ErrInvalidParams = errors.New("rule params are invalid")
	// ErrSnapshotNotFound 快照不存在（resolve 钉住 hash 时不回退 latest）。
	ErrSnapshotNotFound = errors.New("rule snapshot not found")
	// ErrNoApprovedRules 没有任何生效规则：run 拒绝启动，不许用默认限额。
	ErrNoApprovedRules = errors.New("no approved rules")
	// ErrSnapshotMismatch 内容寻址被破坏：derive hash ≠ 声称 hash，或同 hash 不同内容。fail-loud，不吞。
	ErrSnapshotMismatch = errors.New("snapshot hash does not match content")
)

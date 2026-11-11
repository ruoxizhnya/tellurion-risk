package rule

import (
	"errors"

	contractsrisk "github.com/ruoxizhnya/tellurion-contracts/risk"
)

// 领域哨兵。wire code 与它们的映射见 internal/httpapi 与 docs/api-schema.md §2。
//
// 四个跨仓哨兵改为 `contracts/risk` 的别名（ADR-040）：同一条错误值才能让
// `errors.Is` 在 contracts 编码器与本仓调用方之间成立。其余七个是服务私有，
// 跨仓只经 wire code 消费。
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
	// ErrInvalidRuleType 规则类型不在冻结枚举（contracts/risk 别名）。
	ErrInvalidRuleType = contractsrisk.ErrInvalidRuleType
	// ErrInvalidScope 适用范围不在冻结枚举（contracts/risk 别名）。
	ErrInvalidScope = contractsrisk.ErrInvalidScope
	// ErrInvalidParams 参数不是合法 JSON 对象、为空或超限（contracts/risk 别名）。
	ErrInvalidParams = contractsrisk.ErrInvalidParams
	// ErrSnapshotNotFound 快照不存在（resolve 钉住 hash 时不回退 latest）。
	ErrSnapshotNotFound = errors.New("rule snapshot not found")
	// ErrNoApprovedRules 没有任何生效规则：run 拒绝启动，不许用默认限额。
	ErrNoApprovedRules = errors.New("no approved rules")
	// ErrSnapshotMismatch 内容寻址被破坏：derive hash ≠ 声称 hash，或同 hash 不同内容。fail-loud，不吞（contracts/risk 别名）。
	ErrSnapshotMismatch = contractsrisk.ErrSnapshotMismatch
)

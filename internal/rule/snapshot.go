package rule

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

// DomainSnapshotHash 是 risk_rules_hash 的域标签。
//
// 与 DomainVersionHash 同样的归属风险：contracts 无 risk 域，本仓暂为唯一 owner
// （docs/db-schema.md §5）。hash 算法变更 = 域标签 v2，全历史 risk_rules_hash 作废
// （docs/api-schema.md §4）。
const DomainSnapshotHash = "tellurion.risk_snapshot_hash.v1"

// SnapshotRule 是快照里的一条生效规则（active revision 的内容身份）。
// draft 永不进快照：能到达 runtime 的只有 active_revision（docs/dataflow.md §3）。
type SnapshotRule struct {
	RuleID      string          `json:"rule_id"`
	Revision    int             `json:"revision"`
	Scope       Scope           `json:"scope"`
	Type        RuleType        `json:"type"`
	Params      json.RawMessage `json:"params"`
	VersionHash string          `json:"version_hash"`
}

// Snapshot 是内容寻址的规则快照：risk_rules_hash 是回测结果里的可复现锚点。
//
// 不变量（docs/dataflow.md §4）：同一 hash 的规则包字节一致；内容变 ⇒ 必然是新 hash，
// 原地改内容是绝对禁止项（写入与读取都重新 derive 校验）。
type Snapshot struct {
	RiskRulesHash string         `json:"risk_rules_hash"`
	Rules         []SnapshotRule `json:"rules"`
	CreatedAt     time.Time      `json:"created_at"`
}

// Clone 返回深拷贝（Rules 与每个 Params 都拷），避免调用方与 Store 共享可变内存。
func (s Snapshot) Clone() Snapshot {
	out := s
	out.Rules = make([]SnapshotRule, len(s.Rules))
	for i, r := range s.Rules {
		r.Params = append(json.RawMessage(nil), r.Params...)
		out.Rules[i] = r
	}
	return out
}

// SnapshotHash 由快照内容派生 risk_rules_hash：按 rule_id 排序后逐条写进
// 长度前缀域（与 VersionHash 同一写法）。不含 created_at——同一内容永远同一 hash，
// 快照缓存按 hash 永不失效（docs/db-schema.md §3 红线 3）。
func SnapshotHash(rules []SnapshotRule) (string, error) {
	if len(rules) == 0 {
		return "", fmt.Errorf("%w: snapshot must contain at least one active rule", ErrInvalidParams)
	}
	sorted := make([]SnapshotRule, len(rules))
	copy(sorted, rules)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].RuleID < sorted[j].RuleID })
	for i := 1; i < len(sorted); i++ {
		if sorted[i].RuleID == sorted[i-1].RuleID {
			return "", fmt.Errorf("%w: duplicate rule_id %q", ErrInvalidParams, sorted[i].RuleID)
		}
	}

	var b strings.Builder
	b.WriteString(DomainSnapshotHash)
	b.WriteByte('\n')
	writeField(&b, "rule_count", strconv.Itoa(len(sorted)))
	for _, r := range sorted {
		if r.RuleID == "" {
			return "", fmt.Errorf("%w: rule_id is required", ErrInvalidParams)
		}
		if r.Revision <= 0 {
			return "", fmt.Errorf("%w: %s revision must be > 0", ErrInvalidParams, r.RuleID)
		}
		if _, ok := ruleTypes[r.Type]; !ok {
			// 旧读者遇未知类型必须 fail-closed 拒收（docs/ARCHITECTURE.md §9.6）。
			return "", fmt.Errorf("%w: %q", ErrInvalidRuleType, r.Type)
		}
		if _, ok := scopes[r.Scope]; !ok {
			return "", fmt.Errorf("%w: %q", ErrInvalidScope, r.Scope)
		}
		if r.VersionHash == "" {
			return "", fmt.Errorf("%w: %s version_hash is required", ErrInvalidParams, r.RuleID)
		}
		params, err := canonicalParams(r.Params)
		if err != nil {
			return "", err
		}
		writeField(&b, "rule_id", r.RuleID)
		writeField(&b, "revision", strconv.Itoa(r.Revision))
		writeField(&b, "scope", string(r.Scope))
		writeField(&b, "type", string(r.Type))
		writeField(&b, "params", string(params))
		writeField(&b, "version_hash", r.VersionHash)
	}
	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:]), nil
}

// verifyHash 重新 derive 并与声称值比对。写入与读取同检：库（或传输）里存的 hash
// 与内容 derive 值不符即拒，不许 ON CONFLICT DO NOTHING 吞掉（docs/ARCHITECTURE.md §9.5）。
func verifyHash(claimed string, rules []SnapshotRule) error {
	derived, err := SnapshotHash(rules)
	if err != nil {
		return err
	}
	if claimed != derived {
		return fmt.Errorf("%w: derived %s, claimed %q", ErrSnapshotMismatch, derived, claimed)
	}
	return nil
}

// Bundle 是进程内 RiskPolicy 的输入形状：零 I/O、零 HTTP——热路径裁决在 core
// 进程内，规则包经 ParseBundle 进来后不再有任何网络边界（docs/dataflow.md §4 不变量 3）。
type Bundle struct {
	RiskRulesHash string
	rules         []SnapshotRule
	byScope       map[Scope][]SnapshotRule
}

// ForScope 返回某 scope 下的规则。找不到 = 没有规则：调用方按缺规则处理
// （拒单或拒开跑），**不许**回落到默认限额（docs/ARCHITECTURE.md §1）。
func (b Bundle) ForScope(scope Scope) []SnapshotRule {
	rules := b.byScope[scope]
	out := make([]SnapshotRule, len(rules))
	for i, r := range rules {
		r.Params = append(json.RawMessage(nil), r.Params...)
		out[i] = r
	}
	return out
}

// ParseBundle 把 canonical snapshot JSON 变成进程内可用的规则包：
// 重新 derive hash 与 risk_rules_hash 比对（不符 ⇒ fail-closed 拒开跑），
// 未知 type / scope / 非对象参数一律拒收。
func ParseBundle(data []byte) (Bundle, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var snap Snapshot
	if err := dec.Decode(&snap); err != nil {
		return Bundle{}, fmt.Errorf("%w: snapshot is not valid JSON: %v", ErrInvalidParams, err)
	}
	if dec.More() {
		return Bundle{}, fmt.Errorf("%w: snapshot has trailing content", ErrInvalidParams)
	}
	if err := verifyHash(snap.RiskRulesHash, snap.Rules); err != nil {
		return Bundle{}, err
	}
	b := Bundle{
		RiskRulesHash: snap.RiskRulesHash,
		rules:         snap.Clone().Rules,
		byScope:       make(map[Scope][]SnapshotRule),
	}
	for _, r := range b.rules {
		b.byScope[r.Scope] = append(b.byScope[r.Scope], r)
	}
	return b, nil
}

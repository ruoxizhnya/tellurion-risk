package rule

import (
	"bytes"
	"encoding/json"
	"fmt"

	contractsrisk "github.com/ruoxizhnya/tellurion-contracts/risk"
)

// DomainSnapshotHash 是 risk_rules_hash 的域标签，由 `contracts/risk` 唯一持有
// （ADR-040）；本仓只做常量别名，值仍是 `tellurion.risk_snapshot_hash.v1`。
// 改它 = 域标签 v2，全历史 risk_rules_hash 作废（docs/api-schema.md §4）。
const DomainSnapshotHash = contractsrisk.DomainSnapshotHash

// SnapshotRule 是快照里的一条生效规则（active revision 的内容身份）。
// draft 永不进快照：能到达 runtime 的只有 active_revision（docs/dataflow.md §3）。
//
// 形状唯一真相在 `contracts/risk`（ADR-040）；本仓别名引用，不再自持一份。
type SnapshotRule = contractsrisk.SnapshotRule

// Snapshot 是内容寻址的规则快照：risk_rules_hash 是回测结果里的可复现锚点。
//
// 不变量（docs/dataflow.md §4）：同一 hash 的规则包字节一致；内容变 ⇒ 必然是新 hash，
// 原地改内容是绝对禁止项（写入与读取都重新 derive 校验）。
type Snapshot = contractsrisk.Snapshot

// SnapshotHash 由快照内容派生 risk_rules_hash。
//
// 委托 `contracts/risk` 的 `Snapshot.Hash()`（ADR-040 D2）：编码细节只有一份，
// 本仓不再自带实现。不含 created_at——同一内容永远同一 hash，快照缓存按 hash
// 永不失效（docs/db-schema.md §3 红线 3）。
func SnapshotHash(rules []SnapshotRule) (string, error) {
	return contractsrisk.Snapshot{Rules: rules}.Hash()
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
// 未知 type / scope / 非对象参数一律拒收；params 的**键名**按参数字典 fail-closed
// 拒收（未知键 = 词典没有这个字段 ⇒ 消费方无从解释，不许猜），但**不**重校必填与
// 取值域——历史快照按当年词典写入，用今天的取值域卡它会让历史 hash 不可读。
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
		if err := rejectUnknownParamKeys(r.Type, r.Params); err != nil {
			return Bundle{}, err
		}
		b.byScope[r.Scope] = append(b.byScope[r.Scope], r)
	}
	return b, nil
}

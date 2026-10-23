package rule

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
)

// DomainVersionHash 是 rule version_hash 的域标签。
//
// 风险：contracts 的 canonical 域表（tellurion-contracts:engine/canonical.go）没有 risk 域，
// 本仓暂为唯一 owner。若将来 contracts 收编并改用别的字符串，历史 hash 全部失效
// （docs/db-schema.md §5 已登记）。
const DomainVersionHash = "tellurion.risk_rule_version_hash.v1"

// VersionHash 由规则内容派生：rule_id + revision + scope + type + 规范化后的 params。
//
// 不含 description：描述是给人看的注释，改注释不应让规则身份变化。
// 不含 status / 时间：状态跃迁不改变内容身份，同一 revision 反复批准必须得到同一 hash。
func VersionHash(r Revision) (string, error) {
	params, err := canonicalParams(r.Params)
	if err != nil {
		return "", err
	}
	if _, ok := ruleTypes[r.Type]; !ok {
		return "", fmt.Errorf("%w: %q", ErrInvalidRuleType, r.Type)
	}
	if _, ok := scopes[r.Scope]; !ok {
		return "", fmt.Errorf("%w: %q", ErrInvalidScope, r.Scope)
	}
	if r.RuleID == "" {
		return "", fmt.Errorf("%w: rule_id is required", ErrInvalidParams)
	}
	if r.Revision <= 0 {
		return "", fmt.Errorf("%w: revision must be > 0", ErrInvalidParams)
	}

	var b strings.Builder
	b.WriteString(DomainVersionHash)
	b.WriteByte('\n')
	writeField(&b, "rule_id", r.RuleID)
	writeField(&b, "revision", fmt.Sprintf("%d", r.Revision))
	writeField(&b, "scope", string(r.Scope))
	writeField(&b, "type", string(r.Type))
	writeField(&b, "params", string(params))

	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:]), nil
}

func writeField(b *strings.Builder, key, value string) {
	// 长度前缀避免拼接歧义（空串与分隔符不会产生同一 canonical 串）。
	fmt.Fprintf(b, "%d:%s=%d:%s\n", len(key), key, len(value), value)
}

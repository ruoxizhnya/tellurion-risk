package rule

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// ─── 词典形状 ────────────────────────────────────────────────────

// TestParamsDictionary_IsPinned 把 14 个标量（字段名 / 类型 / 单位）钉住。
//
// 词典是跨仓契约的源头（core 按它逐字段映射限额面），漂移必须在这里红，
// 而不是等到消费方算错钱才发现。改这一张表 = 同时改 docs/db-schema.md §7 与
// docs/api-schema.md §1（两处同名同序）。
func TestParamsDictionary_IsPinned(t *testing.T) {
	type want struct {
		name string
		kind ParamKind
		unit string
	}
	expected := map[RuleType][]want{
		RuleTypePositionSizing: {
			{"target_volatility", ParamKindFloat, "年化小数（0.15 = 15%）"},
			{"max_position_weight", ParamKindFloat, "组合净值小数占比（0.10 = 10%）"},
			{"min_position_weight", ParamKindFloat, "组合净值小数占比"},
			{"lookback_days", ParamKindInt, "交易日"},
			{"annualization_factor", ParamKindFloat, "年化因子（√252 ≈ 16.0）"},
		},
		RuleTypeStopLoss: {
			{"atr_period", ParamKindInt, "bar 数"},
			{"base_multiplier", ParamKindFloat, "ATR 倍数"},
			{"bull_multiplier", ParamKindFloat, "ATR 倍数"},
			{"bear_multiplier", ParamKindFloat, "ATR 倍数"},
			{"sideways_multiplier", ParamKindFloat, "ATR 倍数"},
		},
		RuleTypeTakeProfit: {
			{"atr_multiplier", ParamKindFloat, "ATR 倍数"},
		},
		RuleTypeRegime: {
			{"fast_ma_period", ParamKindInt, "bar 数"},
			{"slow_ma_period", ParamKindInt, "bar 数"},
			{"vol_lookback", ParamKindInt, "bar 数"},
		},
	}

	dict := ParamsDictionary()
	total := 0
	for _, ruleType := range []RuleType{
		RuleTypePositionSizing, RuleTypeStopLoss, RuleTypeTakeProfit, RuleTypeRegime,
	} {
		specs, ok := dict[ruleType]
		if !ok {
			t.Fatalf("dictionary must cover rule type %q", ruleType)
		}
		wants := expected[ruleType]
		if len(specs) != len(wants) {
			t.Fatalf("%s: got %d fields, want %d", ruleType, len(specs), len(wants))
		}
		for i, w := range wants {
			got := specs[i]
			if got.Name != w.name || got.Kind != w.kind || got.Unit != w.unit {
				t.Fatalf("%s[%d] = {%s %s %q}, want {%s %s %q}",
					ruleType, i, got.Name, got.Kind, got.Unit, w.name, w.kind, w.unit)
			}
			if !got.Required {
				t.Fatalf("%s.%s must be required (一期全字段必填)", ruleType, got.Name)
			}
			total++
		}
	}
	if total != 14 {
		t.Fatalf("dictionary must pin exactly 14 scalars (core 的 RiskManagerConfig), got %d", total)
	}

	// 调用方拿到的是副本：改它不能改到内部表。
	dict[RuleTypeRegime][0].Name = "hacked"
	if ParamsDictionary()[RuleTypeRegime][0].Name != "fast_ma_period" {
		t.Fatal("ParamsDictionary must return a copy")
	}
}

// ─── 写侧：严格按词典校验 ─────────────────────────────────────────

func TestValidate_RejectsParamsOutsideDictionary(t *testing.T) {
	cases := []struct {
		name   string
		typ    RuleType
		params string
	}{
		{"unknown key", RuleTypeStopLoss,
			`{"atr_period":14,"base_multiplier":2.5,"bear_multiplier":3.5,"bull_multiplier":2.0,"sideways_multiplier":2.5,"unexpected_key":10}`},
		{"percentage form", RuleTypePositionSizing,
			`{"annualization_factor":16.0,"lookback_days":20,"max_position_weight":10,"min_position_weight":0.01,"target_volatility":0.15}`},
		{"missing required", RuleTypeRegime,
			`{"fast_ma_period":10,"slow_ma_period":20}`},
		{"int with fraction", RuleTypeRegime,
			`{"fast_ma_period":10.5,"slow_ma_period":20,"vol_lookback":60}`},
		{"empty object", RuleTypeTakeProfit, `{}`},
		{"non-finite", RuleTypeTakeProfit, `{"atr_multiplier":1e999}`},
		{"fast >= slow", RuleTypeRegime,
			`{"fast_ma_period":20,"slow_ma_period":20,"vol_lookback":60}`},
		{"min > max weight", RuleTypePositionSizing,
			`{"annualization_factor":16.0,"lookback_days":20,"max_position_weight":0.05,"min_position_weight":0.10,"target_volatility":0.15}`},
		{"zero period", RuleTypeRegime,
			`{"fast_ma_period":0,"slow_ma_period":20,"vol_lookback":60}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc, store := newHarness(t)
			_, _, _, err := svc.Create(context.Background(), CreateRequest{
				RuleID: "r", Scope: ScopeGlobal, Type: tc.typ, Params: json.RawMessage(tc.params),
			})
			if !errors.Is(err, ErrInvalidParams) {
				t.Fatalf("want ErrInvalidParams, got %v", err)
			}
			if _, err := store.GetDefinition(context.Background(), "r"); !errors.Is(err, ErrRuleNotFound) {
				t.Fatalf("rejected rule must not be persisted, got %v", err)
			}
		})
	}
}

func TestValidate_AcceptsDictionaryShapes(t *testing.T) {
	cases := []struct {
		name   string
		typ    RuleType
		params string
	}{
		{"position_sizing", RuleTypePositionSizing, sizingParams},
		{"stop_loss", RuleTypeStopLoss, stopLossParams},
		{"take_profit", RuleTypeTakeProfit, takeProfitParams},
		{"regime", RuleTypeRegime, regimeParams},
		{"weight at upper bound", RuleTypePositionSizing,
			`{"annualization_factor":16.0,"lookback_days":20,"max_position_weight":1,"min_position_weight":1,"target_volatility":0.15}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc, _ := newHarness(t)
			if _, _, _, err := svc.Create(context.Background(), CreateRequest{
				RuleID: "r", Scope: ScopeGlobal, Type: tc.typ, Params: json.RawMessage(tc.params),
			}); err != nil {
				t.Fatalf("Create: %v", err)
			}
		})
	}
}

// ─── 读侧：只拒未知键，不重校必填与取值域 ─────────────────────────

func bundleWith(t *testing.T, ruleType RuleType, params string) []byte {
	t.Helper()
	rules := []SnapshotRule{{
		RuleID:      "r-1",
		Revision:    1,
		Scope:       ScopeGlobal,
		Type:        ruleType,
		Params:      json.RawMessage(params),
		VersionHash: strings.Repeat("a", 64),
	}}
	hash, err := SnapshotHash(rules)
	if err != nil {
		t.Fatalf("SnapshotHash: %v", err)
	}
	data, err := json.Marshal(Snapshot{RiskRulesHash: hash, Rules: rules, CreatedAt: fixedNow()})
	if err != nil {
		t.Fatalf("marshal snapshot: %v", err)
	}
	return data
}

func TestParseBundle_RejectsUnknownParamKey(t *testing.T) {
	// 未知键 = 词典里没有这个字段，消费方无从解释 ⇒ 拒绝开跑，不许猜。
	data := bundleWith(t, RuleTypeStopLoss,
		`{"atr_period":14,"base_multiplier":2.5,"bear_multiplier":3.5,"bull_multiplier":2.0,"sideways_multiplier":2.5,"pct":2.5}`)
	if _, err := ParseBundle(data); !errors.Is(err, ErrInvalidParams) {
		t.Fatalf("unknown param key: want ErrInvalidParams, got %v", err)
	}
}

func TestParseBundle_DoesNotRecheckValueDomain(t *testing.T) {
	// 读侧不重校取值域：历史快照按当年词典写入，用今天的域去卡它 =
	// 钉住的历史 risk_rules_hash 不可读（dataflow §4 不变量 8）。
	// 这里用百分数形态（写入侧今天会拒）构造快照，ParseBundle 必须仍能读。
	data := bundleWith(t, RuleTypePositionSizing,
		`{"annualization_factor":16.0,"lookback_days":20,"max_position_weight":10,"min_position_weight":0.01,"target_volatility":0.15}`)
	b, err := ParseBundle(data)
	if err != nil {
		t.Fatalf("read side must not re-check value domains, got %v", err)
	}
	if got := b.ForScope(ScopeGlobal); len(got) != 1 || got[0].RuleID != "r-1" {
		t.Fatalf("bundle content: %+v", got)
	}
}

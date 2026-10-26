package rule

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
)

// ParamKind 是词典字段的机器类型。集合冻结：新增第三种类型 = MINOR，
// 旧读者遇到不认识的类型必须 fail-closed 拒收（docs/ARCHITECTURE.md §9.6）。
type ParamKind string

const (
	ParamKindFloat ParamKind = "float"
	ParamKindInt   ParamKind = "int"
)

// ParamSpec 是规则参数字典的一行：字段名 / 类型 / 单位 / 取值域 / 必填性。
//
// 取值域用区间表达：Min/Max 是端点，MinExclusive/MaxExclusive 决定端点是否可取；
// Max == 0 表示无上界（本词典没有「上界恰为 0」的字段）。
//
// 单位是唯一会让人算错钱的列：权重一律**组合净值小数**（0.10 = 10%），
// 禁止 pct / 百分数形态（docs/db-schema.md §7）。
type ParamSpec struct {
	Name         string
	Kind         ParamKind
	Unit         string
	Min          float64
	MinExclusive bool
	Max          float64
	MaxExclusive bool
	Required     bool
}

// paramsByType 是**规则参数字典的唯一真相源**（文档在 docs/db-schema.md §7 与
// docs/api-schema.md §1，两处同名同序；改这里必须同笔改那两处）。
//
// 键名与 core 的 risk_manager.* 1:1（`tellurion:config/analysis-service.yaml`），
// 改名叫消费方猜——那是本项目算错过钱的缺陷类型。
var paramsByType = map[RuleType][]ParamSpec{
	RuleTypePositionSizing: {
		{Name: "target_volatility", Kind: ParamKindFloat, Unit: "年化小数（0.15 = 15%）",
			Min: 0, MinExclusive: true, Required: true},
		{Name: "max_position_weight", Kind: ParamKindFloat, Unit: "组合净值小数占比（0.10 = 10%）",
			Min: 0, MinExclusive: true, Max: 1, Required: true},
		{Name: "min_position_weight", Kind: ParamKindFloat, Unit: "组合净值小数占比",
			Min: 0, Max: 1, Required: true},
		{Name: "lookback_days", Kind: ParamKindInt, Unit: "交易日",
			Min: 0, MinExclusive: true, Required: true},
		{Name: "annualization_factor", Kind: ParamKindFloat, Unit: "年化因子（√252 ≈ 16.0）",
			Min: 0, MinExclusive: true, Required: true},
	},
	RuleTypeStopLoss: {
		{Name: "atr_period", Kind: ParamKindInt, Unit: "bar 数",
			Min: 0, MinExclusive: true, Required: true},
		{Name: "base_multiplier", Kind: ParamKindFloat, Unit: "ATR 倍数",
			Min: 0, MinExclusive: true, Required: true},
		{Name: "bull_multiplier", Kind: ParamKindFloat, Unit: "ATR 倍数",
			Min: 0, MinExclusive: true, Required: true},
		{Name: "bear_multiplier", Kind: ParamKindFloat, Unit: "ATR 倍数",
			Min: 0, MinExclusive: true, Required: true},
		{Name: "sideways_multiplier", Kind: ParamKindFloat, Unit: "ATR 倍数",
			Min: 0, MinExclusive: true, Required: true},
	},
	RuleTypeTakeProfit: {
		{Name: "atr_multiplier", Kind: ParamKindFloat, Unit: "ATR 倍数",
			Min: 0, MinExclusive: true, Required: true},
	},
	RuleTypeRegime: {
		{Name: "fast_ma_period", Kind: ParamKindInt, Unit: "bar 数",
			Min: 0, MinExclusive: true, Required: true},
		{Name: "slow_ma_period", Kind: ParamKindInt, Unit: "bar 数",
			Min: 0, MinExclusive: true, Required: true},
		{Name: "vol_lookback", Kind: ParamKindInt, Unit: "bar 数",
			Min: 0, MinExclusive: true, Required: true},
	},
}

// ParamsDictionary 返回词典副本（按 RuleType 分组，组内保持声明顺序）。
// 冻结顺序是「同名同序」的一部分：文档表与它逐行对齐，改序 = 改文档。
func ParamsDictionary() map[RuleType][]ParamSpec {
	out := make(map[RuleType][]ParamSpec, len(paramsByType))
	for t, specs := range paramsByType {
		out[t] = append([]ParamSpec(nil), specs...)
	}
	return out
}

// paramSpecs 返回某类型的词典行；类型不在冻结枚举时 ok=false。
func paramSpecs(t RuleType) ([]ParamSpec, bool) {
	specs, ok := paramsByType[t]
	return specs, ok
}

// validateParams 是**写侧**校验：键集合与取值都按词典守。
//
// 入参是 canonical 化后的 params 字节（Revision.Validate 已保证是非空 JSON 对象）。
// 未知键 / 缺必填 / 类型不符 / NaN / Inf / 越出取值域 / 跨字段矛盾一律 ErrInvalidParams
// → wire 400 invalid_params。
func validateParams(t RuleType, canonical []byte) error {
	specs, ok := paramSpecs(t)
	if !ok {
		return fmt.Errorf("%w: %q", ErrInvalidRuleType, t)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(canonical, &raw); err != nil {
		return fmt.Errorf("%w: params must be a JSON object: %v", ErrInvalidParams, err)
	}

	known := make(map[string]struct{}, len(specs))
	for _, spec := range specs {
		known[spec.Name] = struct{}{}
	}
	for key := range raw {
		if _, ok := known[key]; !ok {
			return fmt.Errorf("%w: unknown param %q for rule type %q", ErrInvalidParams, key, t)
		}
	}

	values := make(map[string]float64, len(raw))
	for _, spec := range specs {
		value, present := raw[spec.Name]
		if !present {
			if spec.Required {
				return fmt.Errorf("%w: missing required param %q for rule type %q",
					ErrInvalidParams, spec.Name, t)
			}
			continue
		}
		v, err := parseParamValue(spec, value)
		if err != nil {
			return err
		}
		values[spec.Name] = v
	}
	return validateParamsTogether(t, values)
}

// rejectUnknownParamKeys 是**读侧**的全部校验：只拒词典外的键名。
//
// 与写侧的差别是刻意的：读侧**不**重校必填与取值域——历史快照是按当年的词典写入的，
// 拿今天的取值域去卡它会让钉住的历史 `risk_rules_hash` 不可读（docs/dataflow.md §4
// 不变量 8）。词典的演进纪律因此是「只增不删、不改单位口径」：加键不影响历史，
// 删键 / 改名 / 改单位 = 换域标签（docs/db-schema.md §7）。
func rejectUnknownParamKeys(t RuleType, rawParams json.RawMessage) error {
	specs, ok := paramSpecs(t)
	if !ok {
		return fmt.Errorf("%w: %q", ErrInvalidRuleType, t)
	}
	var raw map[string]json.RawMessage
	dec := json.NewDecoder(bytes.NewReader(rawParams))
	if err := dec.Decode(&raw); err != nil {
		return fmt.Errorf("%w: params must be a JSON object: %v", ErrInvalidParams, err)
	}
	for key := range raw {
		known := false
		for _, spec := range specs {
			if spec.Name == key {
				known = true
				break
			}
		}
		if !known {
			return fmt.Errorf("%w: unknown param %q for rule type %q", ErrInvalidParams, key, t)
		}
	}
	return nil
}

// parseParamValue 把单个字段解析成 float64 并按词典校验类型与取值域。
func parseParamValue(spec ParamSpec, raw json.RawMessage) (float64, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var num json.Number
	if err := dec.Decode(&num); err != nil {
		return 0, fmt.Errorf("%w: param %q must be a number", ErrInvalidParams, spec.Name)
	}
	v, err := strconv.ParseFloat(num.String(), 64)
	if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
		return 0, fmt.Errorf("%w: param %q must be a finite number", ErrInvalidParams, spec.Name)
	}
	if spec.Kind == ParamKindInt && v != math.Trunc(v) {
		return 0, fmt.Errorf("%w: param %q must be an integer", ErrInvalidParams, spec.Name)
	}
	if belowMin(v, spec) || aboveMax(v, spec) {
		return 0, fmt.Errorf("%w: param %q (%v) is outside its domain %s",
			ErrInvalidParams, spec.Name, v, spec.domain())
	}
	return v, nil
}

func belowMin(v float64, spec ParamSpec) bool {
	if spec.MinExclusive {
		return v <= spec.Min
	}
	return v < spec.Min
}

func aboveMax(v float64, spec ParamSpec) bool {
	if spec.Max == 0 {
		return false
	}
	if spec.MaxExclusive {
		return v >= spec.Max
	}
	return v > spec.Max
}

// domain 把取值域渲染成人读区间（错误信息与文档同字）。
func (s ParamSpec) domain() string {
	left, right := "[", "]"
	if s.MinExclusive {
		left = "("
	}
	if s.Max == 0 {
		return fmt.Sprintf("%s%v,+∞)", left, s.Min)
	}
	if s.MaxExclusive {
		right = ")"
	}
	return fmt.Sprintf("%s%v,%v%s", left, s.Min, s.Max, right)
}

// validateParamsTogether 是跨字段约束：单字段各自合法、合起来仍可能矛盾。
func validateParamsTogether(t RuleType, values map[string]float64) error {
	switch t {
	case RuleTypePositionSizing:
		maxW, hasMax := values["max_position_weight"]
		minW, hasMin := values["min_position_weight"]
		if hasMax && hasMin && minW > maxW {
			return fmt.Errorf("%w: min_position_weight (%v) must not exceed max_position_weight (%v)",
				ErrInvalidParams, minW, maxW)
		}
	case RuleTypeRegime:
		fast, hasFast := values["fast_ma_period"]
		slow, hasSlow := values["slow_ma_period"]
		if hasFast && hasSlow && fast >= slow {
			return fmt.Errorf("%w: fast_ma_period (%v) must be less than slow_ma_period (%v)",
				ErrInvalidParams, fast, slow)
		}
	}
	return nil
}

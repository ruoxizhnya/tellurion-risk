package rule

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
)

// FactorySeedActor 是出厂规则集写入审计时使用的 actor 标记。
//
// 它把「seed 规则」与「人工创建的规则」在 risk.audit_logs 里区分开：
//
//	SELECT DISTINCT rule_id FROM risk.audit_logs
//	 WHERE action = 'create' AND actor = 'factory-seed';
//
// 人工操作的 actor 来自鉴权 claims（缺失时退 subject / open-access），
// 不会等于本常量；seed 之后的人工修改会追加 actor 为人工身份的 update / approve 行，
// 审计历史因此能看出「哪条规则是出厂、后来被谁改过」。
const FactorySeedActor = "factory-seed"

// FactorySeedVersion 标识出厂规则集的内容版本。规则集或参数变化时递增，
// 便于启动日志与排障时判断「库里的规则是哪一版 seed 灌进来的」。
const FactorySeedVersion = 1

// SeedReport 是一次 seed 的结果，供启动日志与测试断言使用。
type SeedReport struct {
	// Created 是本次新建并激活的 rule_id。
	Created []string
	// Skipped 是因 rule_id 已存在（无论内容 / 状态）而跳过的 rule_id。
	Skipped []string
}

// factorySeedRules 是出厂规则集。
//
// 参数值于 2026-11-12 从部署库 risk.rule_revisions 逐字段捕获（工程默认值，
// 不是投资建议；施工记录见 docs/TASKS.md RISK-RULE-SEED）。参数保持原值，
// 快照 hash 才能复现 core CORE-COMPOSE-UNIFY 观测到的 84a95d7d… 基线。
//
// 描述不参与 version_hash / risk_rules_hash（ADR-0002），这里写成面向运维的
// 出厂说明，而不是部署库里遗留的 E2E 标签。
var factorySeedRules = []CreateRequest{
	{
		RuleID: "global-position-sizing-v1",
		Scope:  ScopeGlobal,
		Type:   RuleTypePositionSizing,
		Params: json.RawMessage(`{
			"annualization_factor": 15.87,
			"lookback_days": 25,
			"max_position_weight": 0.12,
			"min_position_weight": 0.02,
			"target_volatility": 0.22
		}`),
		Description: "出厂规则：波动率目标仓位（工程默认，非投资建议）",
		Actor:       FactorySeedActor,
	},
	{
		RuleID: "global-regime-v1",
		Scope:  ScopeGlobal,
		Type:   RuleTypeRegime,
		Params: json.RawMessage(`{
			"fast_ma_period": 12,
			"slow_ma_period": 26,
			"vol_lookback": 80
		}`),
		Description: "出厂规则：均线市场状态识别（工程默认，非投资建议）",
		Actor:       FactorySeedActor,
	},
	{
		RuleID: "global-stop-loss-v1",
		Scope:  ScopeGlobal,
		Type:   RuleTypeStopLoss,
		Params: json.RawMessage(`{
			"atr_period": 20,
			"base_multiplier": 3,
			"bear_multiplier": 4,
			"bull_multiplier": 2.2,
			"sideways_multiplier": 2.8
		}`),
		Description: "出厂规则：ATR 止损（工程默认，非投资建议）",
		Actor:       FactorySeedActor,
	},
	{
		RuleID:      "global-take-profit-v1",
		Scope:       ScopeGlobal,
		Type:        RuleTypeTakeProfit,
		Params:      json.RawMessage(`{"atr_multiplier": 3.5}`),
		Description: "出厂规则：ATR 止盈（工程默认，非投资建议）",
		Actor:       FactorySeedActor,
	},
}

// SeedFactory 把出厂规则集灌入空库，让新部署无需手写规则即可通过 resolve。
//
// 语义（幂等 / 可重放 / 不覆盖人工改动）：
//   - 只要 rule_id 已存在（无论内容、状态、是否人工修改），就跳过，绝不 update；
//   - 每条规则在**一个 Store.Tx 事务**里完成 create + approve，终态 active：
//     中途失败整条回滚，不会留下「已创建未审批」的半套规则；
//   - 被删除的出厂规则会在下一次启动时补回，其余已存在规则不受影响；
//   - 走与手工操作相同的校验（Validate / 参数字典）与审批状态机（draft→approved→active），
//     审计 actor 固定为 FactorySeedActor。
//
// 并发启动的多个实例不会互相破坏：后插入者会拿到唯一键冲突，按「已存在」跳过
// （先插入者已在同一事务里完成 approve，规则终态一致）。
func (s *Service) SeedFactory(ctx context.Context) (SeedReport, error) {
	report := SeedReport{Created: []string{}, Skipped: []string{}}
	for _, req := range factorySeedRules {
		created, err := s.seedOne(ctx, req)
		if err != nil {
			return report, fmt.Errorf("factory seed %s: %w", req.RuleID, err)
		}
		if created {
			report.Created = append(report.Created, req.RuleID)
		} else {
			report.Skipped = append(report.Skipped, req.RuleID)
		}
	}
	return report, nil
}

// seedOne 在单个事务里完成一条规则的 create + approve。
// 返回 created=false 表示该 rule_id 已存在（含并发实例刚插入的情况）。
func (s *Service) seedOne(ctx context.Context, req CreateRequest) (bool, error) {
	var created bool
	err := s.store.Tx(ctx, func(st Store) error {
		if _, err := st.GetDefinition(ctx, req.RuleID); err == nil {
			return nil // 已存在：跳过，绝不覆盖（人工改过的规则保持原样）
		} else if !errors.Is(err, ErrRuleNotFound) {
			return err
		}
		if _, _, _, err := s.create(ctx, st, req); err != nil {
			return err
		}
		if _, _, err := s.approve(ctx, st, ApproveRequest{
			RuleID:   req.RuleID,
			Revision: 1,
			Actor:    req.Actor,
		}); err != nil {
			return err
		}
		created = true
		return nil
	})
	if err != nil {
		// 并发实例同时 seed 同一条规则：另一个实例先插入 ⇒ 唯一键冲突。
		// 对方在同一事务里 create+approve，提交后规则即为 active；按「已存在」跳过。
		if errors.Is(err, ErrIdempotencyConflict) {
			return false, nil
		}
		return false, err
	}
	return created, nil
}

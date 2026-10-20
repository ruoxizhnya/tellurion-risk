---
status: active
last-verified: 2026-10-20
verified-by: 功能边界固化（三份契约：db / api / dataflow）
---

# 架构 — tellurion-risk

## 1. 定位

风控规则的**真相源** + 聚合监控与熔断。

关键判断：**同步裁决留在 core 进程内**。风控出仓的是"规则怎么定义、版本怎么管、跨策略敞口怎么看"，不是"每笔订单要不要放行"。

## 2. 分层

```
写侧（人 / 审批）                     读侧（内核 / 实盘）
  rule → revision → approve            resolve → bundle（run 前 1 次）
  → version_hash                       → RiskPolicy（进程内）
        │                                     │
        ↓                                     ↓
   risk.rule_defs / _revisions / _snapshots / limits / events / breakers / audit

   监控层：exposure / alerts / evaluate（聚合，异步）
```

## 3. 组件职责

| 组件 | 职责 | 不做 |
|---|---|---|
| 规则管理 | 定义 / 版本 / 审批状态机 | 不执行裁决 |
| 快照 | 内容寻址、永不失效 | 不解析规则语义 |
| 事件收集 | 异步批量落库 | 不在热路径 |
| 监控 | 敞口 / 告警 / 合规检测 | 不阻断下单（阻断靠熔断指令） |

## 4. 三份契约（功能的边界就在这三份文件里）

| 维度 | 文件 | 回答什么问题 |
|---|---|---|
| 数据形状 | [`db-schema.md`](db-schema.md) | 本仓拥有哪些表？谁能写？ |
| 接口形状 | [`api-schema.md`](api-schema.md) | 对外暴露什么？错误码与幂等怎么做？ |
| 流向与顺序 | [`dataflow.md`](dataflow.md) | 数据怎么流？哪里 fail-closed？ |

## 5. 依赖方向

```
contracts（规则 DTO / 事件 DTO）+ kit（logging/http/db）
        ↑
 tellurion-risk ──(HTTP 粗粒度)──► core 进程内 RiskPolicy
        └──(HTTP 异步)──► 事件回传
```

## 6. 关键决策

- [`adr/0001-两层结构-真相源出仓裁决留内核.md`](adr/0001-两层结构-真相源出仓裁决留内核.md)

## 7. 非目标（明确不做）

- 不做同步裁决服务
- 不持有订单/持仓
- 不做券商对接
- 不做回测

## 8. 分期与状态

| 阶段 | 内容 | 状态 |
|---|---|---|
| X3a | 规则定义 + 版本 + 审批状态机 | ⬜ |
| X3b | 快照 + `resolve` / `bundle` | ⬜ |
| X3c | 事件回传 + 监控聚合 | ⬜ |
| X3d | 熔断指令（幂等） | ⬜ |

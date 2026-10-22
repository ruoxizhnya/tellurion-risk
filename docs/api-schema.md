---
status: active
last-verified: 2026-10-22
verified-by: M8 接口对表（QT-42 前置 ②）
---

# API Schema — tellurion-risk

> **实现状态（2026-10-20）**：本仓目前**只有占位 `doc.go`，没有任何 handler**——下列端点**全部未实现**。
> 本文件是**目标契约**，不是现状说明书；开工顺序见 `docs/TASKS.md` 的「施工序」。

> **契约地位**：HTTP，但**只有粗粒度操作走 HTTP**（run 前解析、事件回传、熔断、监控）。热路径的 `CheckOrder` 是进程内 port。
> 本文件是**本仓对外接口的唯一真相源**。代码与它不一致时，先判断谁错：契约错 → 改本文 + 立 ADR；实现错 → 改代码。
> 跨仓消费方（core / agent / ui / 其他服务）按本文件编码，**不读本仓实现**。
> **请求 / 响应形状**：本文只冻结**路径、错误码、幂等与兼容**；逐端点的目标签名、DTO 字段与版本策略见 `ARCHITECTURE.md` §9（M8 接口对表），两处必须同步改，不复制形状。

## 1. 接口面

### 规则管理（写侧，人 / 审批）

| 方法 | 路径 | 用途 |
|---|---|---|
| POST | `/api/risk/rules` | 建规则（draft） |
| PUT | `/api/risk/rules/{id}` | 改规则 → 生成新 revision |
| POST | `/api/risk/rules/{id}/approve` | 审批激活（产生新 `version_hash`） |
| GET | `/api/risk/rules?scope=&active=true` | 查询 |
| GET | `/api/risk/snapshots/{hash}` | 取规则快照 |

### 运行接入（读侧，内核 / 实盘）

| 方法 | 路径 | 用途 |
|---|---|---|
| POST | `/api/risk/resolve` | 规则集引用（锁定 hash 或 latest）→ 快照元数据 |
| GET | `/api/risk/snapshots/{hash}/bundle` | 拉规则包（run 前 1 次，按 hash 缓存永不失效） |
| POST | `/api/risk/events` | 回传裁决事件（异步批量，**非热路径**） |
| POST | `/api/risk/breakers` | 下发熔断指令（幂等键） |

### 监控（聚合层）

| 方法 | 路径 | 用途 |
|---|---|---|
| GET | `/api/risk/monitor/exposure` | 跨策略 / 账户敞口 |
| GET | `/api/risk/monitor/alerts` | 告警列表 |
| POST | `/api/risk/monitor/evaluate` | 手动触发检测（compliance 六检测器 + 减持 + 大额） |

## 2. 错误码与 fail-closed 语义

> 错误码与 `ARCHITECTURE.md` §9.4 **双向一一对应**；本节是 code 的**唯一词表**，新增 code 必须先改这里。

| 情况 | 返回 | 调用方行为 |
|---|---|---|
| 无凭证 / token 过期 | 401 `unauthenticated`（body 随 QT-50 ④ 统一面） | 不重试，补齐凭证 |
| 角色不足（写侧非 operator / 读侧非 service） | 403 `forbidden` | 不重试 |
| 规则类型不在冻结枚举 / 参数非法 | 400 `invalid_rule_type` / `invalid_params` | 修正请求，不重试 |
| 同一幂等键（`rule_id` / `breaker_id`）不同内容 | 409 `idempotency_conflict` | **当冲突处理**，不许当成功 |
| 规则不存在 | 404 `rule_not_found` | 不重试 |
| 编辑基准 `base_revision` 过期 | 409 `revision_conflict` | 重读 head 后再改 |
| 编辑已废弃规则 | 409 `rule_deprecated` | 不重试 |
| 审批的 revision 不是 draft | 409 `revision_not_draft` | 不重试 |
| 无 approved 版本 | 404 `no_approved_rules` | run 前拒绝启动 |
| 规则 hash 不存在 | 404 `snapshot_not_found` | 拒绝（不回退 latest） |
| 熔断指令重复（同键同载荷） | 200 + `already_applied` | 视为成功（幂等） |
| 事件批量部分失败 | 207 + `partial_failure` + 失败明细 | 重试失败项，不重发全量 |

**fail-closed**：拿不到规则快照 = 不许开跑，不允许"先用默认限额"；没有凭证 / 角色不足 = 不服务，**不静默降级到匿名或 loopback 放行**（loopback-only insecure 仅限显式本地开发配置）。

## 3. 幂等性与重试

- `POST /rules` 的幂等键是 `rule_id`（调用方给）：同键同内容 → 200 + 现有 revision；同键不同内容 → 409 `idempotency_conflict`。
- `PUT /rules/{id}` 必须带 `base_revision`：不匹配当前 head → 409 `revision_conflict`（防丢更新）。
- `POST /rules/{id}/approve` 对**同一 revision** 重放幂等（同 `version_hash`）；状态机之外没有第二条路。
- `POST /breakers` 必须带幂等键（`breaker_id`），同键同载荷重复调用不重复执行；同键不同载荷 → 409。
- `POST /events` 支持批量 + 去重键（`run_id + order_id + event_seq`）；部分失败返回 207，只重发失败项。
- `resolve` / `snapshots` / `rules`（GET）只读，可任意重试。

## 4. 版本与兼容

> 逐端点 / 逐符号的版本策略见 `ARCHITECTURE.md` §9.6；本节只留三条总则。

- 规则**类型**（枚举）：新增类型 = MINOR（旧读者必须 fail-closed 拒收未知类型）；改既有类型的语义 = breaking（老 run 的 `risk_rules_hash` 可比性失效）。
- 端点只增不删；废弃走 `deprecated` 头 + 一个 MINOR 周期。
- 快照格式变更必须保留旧格式读取器（否则历史回测不可复现）；hash 算法变更 = 域标签 v2，全历史 hash 作废。

## 5. 不提供什么（边界）

- 不做逐笔 / 逐 bar 的同步裁决（进程内 port）
- 不持有订单 / 持仓真相
- 不做券商通道对接（那是 broker）
- 不做回测（只提供"这次 run 该用哪套规则"）
- **不接管** core :8085 的四条进程内 risk 端点（`/api/risk/calculate_position`、`detect_regime`、`check_stoploss`、`metrics`）——它们是 in-process `RiskManager` 的调试面，热路径不经过 HTTP；本仓只拥有 `rules` / `snapshots` / `resolve` / `events` / `breakers` / `monitor`（§9.1 F4 / R1）。

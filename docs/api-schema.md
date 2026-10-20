---
status: active
last-verified: 2026-10-20
verified-by: 功能边界固化（三份契约：db / api / dataflow）
---

# API Schema — tellurion-risk

> **实现状态（2026-10-20）**：本仓目前**只有占位 `doc.go`，没有任何 handler**——下列端点**全部未实现**。
> 本文件是**目标契约**，不是现状说明书；开工顺序见 `docs/TASKS.md` 的「施工序」。

> **契约地位**：HTTP，但**只有粗粒度操作走 HTTP**（run 前解析、事件回传、熔断、监控）。热路径的 `CheckOrder` 是进程内 port。
> 本文件是**本仓对外接口的唯一真相源**。代码与它不一致时，先判断谁错：契约错 → 改本文 + 立 ADR；实现错 → 改代码。
> 跨仓消费方（core / agent / ui / 其他服务）按本文件编码，**不读本仓实现**。

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

| 情况 | 返回 | 调用方行为 |
|---|---|---|
| 无 approved 版本 | 404 + `no_approved_rules` | run 前拒绝启动 |
| 规则 hash 不存在 | 404 + `snapshot_not_found` | 拒绝（不回退 latest） |
| 熔断指令重复 | 200 + `already_applied` | 视为成功（幂等） |
| 事件批量部分失败 | 207 + 失败明细 | 重试失败项，不重发全量 |

**fail-closed**：拿不到规则快照 = 不许开跑，不允许"先用默认限额"。

## 3. 幂等性与重试

- `POST /breakers` 必须带幂等键（`breaker_id`），重复调用不重复执行。
- `POST /events` 支持批量 + 去重键（`run_id + order_id + event_seq`）。
- `resolve` / `snapshots` 只读，可任意重试。

## 4. 版本与兼容

- 规则**类型**（枚举）变更算 breaking：老 run 的 `risk_rules_hash` 必须仍能解析。
- 端点只增不删；废弃走 `deprecated` 头 + 一个 MINOR 周期。
- 快照格式变更必须保留旧格式读取器（否则历史回测不可复现）。

## 5. 不提供什么（边界）

- 不做逐笔 / 逐 bar 的同步裁决（进程内 port）
- 不持有订单 / 持仓真相
- 不做券商通道对接（那是 broker）
- 不做回测（只提供"这次 run 该用哪套规则"）

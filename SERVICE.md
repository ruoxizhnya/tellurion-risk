---
service-card: 1
repo: tellurion-risk
kind: service
lifecycle: production
owner: ruoxizhnya
impact: degrading

requires:
  - tellurion-kit
required-by: []
calls: []
called-by:
  - tellurion

truth:
  readme: README.md
  agents: AGENTS.md
  architecture: docs/ARCHITECTURE.md
  api: docs/api-schema.md
  db: docs/db-schema.md
  dataflow: docs/dataflow.md
  runbook: docs/runbook.md
  tasks: docs/TASKS.md

status: evergreen
last-verified: 2026-11-11
verified-by: 2026-11-11 实读 README / AGENTS / ARCHITECTURE / api-schema / db-schema / dataflow / runbook / go.mod / deploy/compose.yaml，并核对 core 的 risk_service 开关；端口、端点数量、表数量未写入
---

# 服务卡：tellurion-risk

> **一句话**：风控规则的真相源——规则 / 审批 / 快照 / 审计 / 事件聚合 / 熔断指令；同步裁决仍在 core 进程内。
>
> `service` · `production` · `degrading` · `ruoxizhnya`

## 1. 这是什么

它管理「规则长什么样、哪一版生效、谁改的」：规则定义与审批状态机、内容寻址的版本 hash、快照 resolve、审计读，以及事件回传后的聚合监控（暴露 / 告警 / 评估投影）与熔断指令。它**不**在逐笔 / 逐 bar 的路径上做裁决——那部分 `RiskPolicy.CheckOrder` 永驻 core。

**它拥有**：`risk` schema 的 DDL 与唯一写权（规则、快照、审计、事件、熔断指令）；快照内容的 `version_hash` / `risk_rules_hash` 生产。
**它不拥有**：订单 / 成交 / 持仓 / 资金；告警检测器（6 个在 core）；同步裁决逻辑；行情与因子。

## 2. 它不做什么

- 不做同步裁决：`CheckOrder` / 止损 / regime 检测留在 core 进程内，禁止远程调用。
- 不把告警检测器搬过来：本仓只做通道编排、聚合与报送，检测在 core。
- 不写 core / data / strategy 的任何表；不碰订单、成交、持仓、资金。
- 不在未装配 Authorizer 时放行：全端点 fail-closed，未装配即 503。
- 不替调用方改跨仓开关与凭证（`resolve_enabled` / `events_enabled` 归 core）。

## 3. 与谁相连

### 调用它（入边）

| 来源 | 协议 | 用来做什么 | 本仓挂了会怎样 |
|---|---|---|---|
| `tellurion` | HTTP | run 提交时 `resolve` 取 hash；启动 / 装配期取限额快照；检测器按 `POST /events` 回传告警；对账差异回传 | 未开 `resolve_enabled` 时主链路不受影响；开启后 core 启动或 run 提交 fail-closed 失败，不静默用空规则 |

> 服务间 `service` 角色只开 `resolve` 与 `events`；`/audit` 与规则管理面不对机器角色开放。

### 它调用（出边）

| 目标 | 协议 | 用来做什么 | 目标挂了会怎样 |
|---|---|---|---|
| PostgreSQL | PG | `risk.*` 的 DDL、事务读-改-写、聚合读 | `/readyz` 503；业务端点报非 2xx，不降级成空集 |
| 无内部服务仓 | — | — | — |

> 不写 URL、端口、超时。地址见 `deploy/compose.yaml` 与 core 的 `config/`；接口形状见 `docs/api-schema.md`。

## 4. 运行时身份

- **常驻进程**：`cmd/riskservice`（唯一 HTTP 监听）。
- **存活/就绪语义**：
  - `GET /healthz`：进程存活，**不读库**。
  - `GET /readyz`：PG 可连 **且** `rule.Migrations()` 已执行；内存档响应标 `store:"memory"`。
  - 探针公开、不鉴权、不限流；业务端点按调用方身份限流，鉴权先于限流。
- **鉴权**：有 `JWT_SECRET` 时启用；无 secret + 回环开放，无 secret + 非回环**拒绝启动**。
- **部署真相**：`deploy/compose.yaml`、`Dockerfile`、`README.md` 快速开始。
- **端口**：不在此卡；见上述部署真相。

## 5. 数据与契约归属

| 维度 | 归属 | 真相源 |
|---|---|---|
| DB schema | `risk` 的唯一写者与 DDL | `docs/db-schema.md` |
| 跨仓 DTO / 契约 | 快照 Go 形状登记在 `tellurion-contracts/risk`，core 消费它 | `tellurion-contracts:docs/API.md` |
| 对外 HTTP | 规则 / 快照 / 审计 / 事件 / 熔断 / 监控 | `docs/api-schema.md` |

> **已裁（2026-11-11，ADR-040）**：快照形状 / 冻结词表 / `DomainSnapshotHash` / 四个跨仓哨兵的唯一真相 = `contracts/risk`，本仓改**类型别名 + 委托**（`SnapshotHash` → `Snapshot.Hash()`）；`Bundle` / `ParseBundle` / `ForScope` 与参数字典仍留本仓（服务策略）。`go.mod` 已 pin `contracts v0.10.0`。见 `tellurion-docs:docs/adr/adr-040-risk-snapshot-single-source.md`。

## 6. 坏了怎么办

- **影响面**：`degrading`——默认部署（`resolve_enabled=false`）主链路不受影响；开启 resolve 的部署里，core 启动与 run 提交 fail-closed 失败；事件回传失败只丢告警投影，不影响已裁定的交易。
- **先确认（按顺序）**：
  1. `GET /healthz` 是否 200；不是则看进程日志与启动配置（尤其无 secret + 非回环拒起）。
  2. `GET /readyz` 是否 200；503 时看 PG 连通与 `rule.Migrations()` 是否执行。
  3. 监控三端点返回空集时先看 `configured`：`false` = 未接入，`true` + 空 = 已接入未触发；读失败是非 2xx，不是空集。
- **升级路径**：`owner` → 跨仓争议 → core `docs/TASKS.md`
- **runbook**：`docs/runbook.md`（起停 §1、探针 §2、审计 / 迁移、熔断人工解除、空集判读）。

## 7. 5 分钟后去哪

| 我想…… | 读这个 |
|---|---|
| 知道规则 / 快照 / 审计 / 事件 / 熔断 / 监控端点的形状、错误码与失败语义 | `docs/api-schema.md` |
| 知道 `risk.*` 有哪些表、参数字典与取值域 | `docs/db-schema.md` |
| 知道事件怎么回传、聚合怎么读 | `docs/dataflow.md` |
| 起停 / 熔断解除 / 空集判读 | `docs/runbook.md` |
| 当前在做什么 / 已知缺口 | `docs/TASKS.md` |
| 架构与决策（含 ADR-0005 / 0006 鉴权口径） | `docs/ARCHITECTURE.md` / `docs/adr/` |

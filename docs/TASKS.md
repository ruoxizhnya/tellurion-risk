---
status: active
last-verified: 2026-11-12
verified-by: 2026-11-12 挂起任务处置批次（主任指令）——`RISK-RULE-SEED` ✅ 完成（启动时 seed 4 条出厂 `global` 规则；幂等 / 不覆盖人工改动 / 删除单条重启补回；空库真 PG + 真二进制 resolve 复现 `84a95d7d…`，见归档 §五）、`RISK-CREDENTIAL-PATH` ✅ 收口（与 core 侧同一证据批次，200/401/403，证据落 core 归档 `a916a51`）；`RISK-EVENTS-COVERAGE` 维持 ⏸（events_enabled 出厂 false）。此前： 2026-11-12 `X-DEP-BUMP-CONSUMERS` 回补 `govulncheck` 门（首轮 `c19634a` 只升 pin 未加门；本次补 CI 步并真做降版破坏验证，归档条目状态由「部分完成」改回 ✅）；W7 四单收口（`RISK-GRACEFUL-SHUTDOWN` / `RISK-PGDOWN-503` / `RISK-TEST-DSN-UNIFY` / `RISK-GOMOD-DIRECT`：代码 + 文档 + 破坏验证 + 真栈演练，原文与施工记录移入 archive）——此前 2026-11-11 全面审查 W7 台账立单（A5 授权）——新立 4 条六字段单（`RISK-GRACEFUL-SHUTDOWN` / `RISK-PGDOWN-503` / `RISK-TEST-DSN-UNIFY` / `RISK-GOMOD-DIRECT`），证据坐标引 ODR-069 / ODR-072（`tellurion-docs:docs/archive/odr/`）；本轮只立单不修复。此前 2026-11-11 新增 `RISK-CONTRACTS-SNAPSHOT-MIGRATION`（跨仓 ADR-040：`contracts/risk` 为形状唯一真相，本仓改别名引用，`Bundle`/`ParseBundle` 留本仓）；`QT-42` ✅ 订正（2026-11-10：正文「收口记录」早已判 ✅，标题漏改；四条未落项均在 ⑥ 边界外且由 CT-03 / SVC-CORE-RISK-RETIRE / 数值口径裁决 / `risk.limits` 遗留空表裁决关闭，本仓零代码改动）；`SVC-RISK-G3` 收口（2026-11-10：core `risk_manager.*` 读点 14→0、core 新增新旧限额逐字段对拍测试、真 PG + 真 risk 进程的告警回传跑出 `risk.risk_events` 真实行与 `monitor/alerts count:1`、停服后 outbox `failed/attempts=3` 不静默丢；本仓零代码改动）；`SVC-RISK-G4` 收口（2026-11-10：core 侧停服前后各一次真退出码，补齐 ⑤③）；`QT-74` 收口（2026-11-10：迁 contracts 由 CT-03、core 消费由 SVC-CORE-RISK-RETIRE 关掉，数值口径 1:1 零换算，`risk.limits` 遗留空表）；`SVC-RISK-G1` 收口（2026-11-10：Docker 引擎 29.8.0 起来后 `docker compose up --wait` EXIT=0、pg/risk 双 healthy，`/healthz` 200、`/readyz` 200，PG 停→503、恢复→200，翻 ✅）；QT-42 补丁 —— PG Store 审批跃迁修复（`updateRule` 先覆盖后插入）+ 首条真 PG 回归腿 `internal/pgstore/approve_pg_test.go`（2026-11-07，破坏验证真做真还原）；QT-80 施工 —— risk_events.actor 留痕（claims 取值 + 两条用例 + 破坏验证 + 真 PG 两条集成腿，2026-11-07 补齐后 ✅）；SVC-RISK-G4 施工（2026-11-07）—— 失败语义五类 / 超时重试口径 / monitor `configured` 判别位 / runbook / 本仓侧停服演练；此前 SVC-RISK-G0 完成（Dockerfile + deploy/compose + 默认 8089 与 TCP 探针）；**2026-11-10**：本机 PostgreSQL 16.15 @ `127.0.0.1:5432` 上补齐真 PG 腿（`internal/pgstore` 全量用例全 PASS、`go test ./...` 6 包 ok、runbook 的 psql 审计/熔断/表存在三条腿）；Docker/compose 腿随后（同日）补跑通过，见 `SVC-RISK-G1`
completed-2026-11-12: `RISK-RULE-SEED`（`feat(risk): RISK-RULE-SEED 出厂规则集 seed`）/ `RISK-GRACEFUL-SHUTDOWN`（`b547110`）/ `RISK-PGDOWN-503`（`d94358e`）/ `RISK-TEST-DSN-UNIFY`（`27a3e48`）/ `RISK-GOMOD-DIRECT`（`4a14b9c`）✅，原文与施工记录在 `docs/archive/tasks-archive-2026-11.md`
completed-2026-11-11: `RISK-CONTRACTS-SNAPSHOT-MIGRATION` ✅（快照形状 / 冻结词表 / `DomainSnapshotHash` / 四个跨仓哨兵改 `contracts/risk` 别名 + `SnapshotHash` 委托；`go.mod` pin `v0.10.0`；对拍腿 `TestSnapshotHash_DelegatesToContracts`；原文与施工记录在 `docs/archive/tasks-archive-2026-11.md`）
---

# TASKS — tellurion-risk 未完成项

> **归档说明（2026-11-11）**：本文件已瘦身为「只装未做的事」。2026-11-10 及以前已收口（✅）的任务原文、施工记录、验收输出与破坏验证，已逐字移入 [`docs/archive/tasks-archive-2026-11.md`](archive/tasks-archive-2026-11.md)；该文件顶部有「检索索引」表（ID / 标题 / 收口日期 / commit / 证据链接）。检索：`grep -n "<任务ID或commit>" docs/archive/tasks-archive-2026-11.md`。
>
> 状态：`⬜ 待做` / `🔶 进行中` / `⛔ 阻塞（有未解除前置）` / `⏸ 挂起（外部依赖或已裁决暂不实施；**不计入开放项**，重开须另立单并满足该条「重开条件」）` / `✅ 完成（附日期，随即移入归档）`

## 一、未做 / 挂起 / 外部依赖

> 本节 = 本轮（2026-11-11）台账重整时登记的遗留边界。此前已收口单的正文与施工记录全部在归档文件里。

### RISK-EVENTS-COVERAGE · 事件回传链路（`risk_service.events_enabled`）未覆盖 [P2] ⏸

- **① 目标**：把「core 打开 `risk_service.events_enabled` → 事件投递到本仓 `/api/risk/events`」这条链真跑一次。
- **② 上下文**：出厂 `events_enabled=false`（`tellurion:config/analysis-service.yaml:211`），本仓 `internal/httpapi/monitor.go` 的 `configured:false` 分支与 `api.go` 的「本仓看不见 core 的开关」注释都说明这条链默认不在场上；`SVC-RISK-G3` 只验证了告警 outbox 的 `pending → sent / failed`，**没有**验证 core 真把事件 POST 过来。
- **③ 要求**：① 真栈打开 `events_enabled=true`，跑一次告警事件从 core outbox 到本仓落库；② 覆盖 401 / 超限 / 重投三种边界；③ 结果写进本节或另立收口记录。
- **④ 约束与非目标**：不改报文形状与状态词表；不引入 MQ。
- **⑤ 验收**：本仓侧能看到 core 投递的真实事件行；破坏验证 = 关掉开关 ⇒ 本仓 `/monitor` 报 `configured:false`。
- **⑥ 边界**：接收面归本仓；投递面归 `tellurion`（`cmd/analysis/wiring/risk_events.go`）。
- **重开条件**：事件回传从「默认关」转为任一部署默认开。

## 二、跨仓指针（不在本仓，勿双写）

| 事项 | 去哪 |
|---|---|
| core 侧风控快照消费、热更新与凭证路径 | `tellurion:docs/TASKS.md` → `CORE-RISK-SNAPSHOT-RELOAD` / `CORE-RISK-CREDENTIAL-PATH` |
| 快照形状与冻结词表 | `tellurion-contracts:docs/TASKS.md` |

## 维护本文件

- **新增单**：写全六字段；六字段里必须显式引用**冻结契约的路径与节号**（契约仓的包路径、设计文档的节号），不许写"参考相关实现"。
- **派工前先证伪前提**：读码核对工单写的前提；不符就停下回报 + 给拆法，不硬做、不扩范围。
- **跨仓引用写 `repo:path`**（如 `tellurion:docs/TASKS.md`），不做可点击相对链接。
- **顺序纪律**：同一波次内撞同一批文件的单按单号串行，一单一 commit。

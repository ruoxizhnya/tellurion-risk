---
status: active
last-verified: 2026-11-12
verified-by: 2026-11-12 W7 四单收口（`RISK-GRACEFUL-SHUTDOWN` / `RISK-PGDOWN-503` / `RISK-TEST-DSN-UNIFY` / `RISK-GOMOD-DIRECT`：代码 + 文档 + 破坏验证 + 真栈演练，原文与施工记录移入 archive）——此前 2026-11-11 全面审查 W7 台账立单（A5 授权）——新立 4 条六字段单（`RISK-GRACEFUL-SHUTDOWN` / `RISK-PGDOWN-503` / `RISK-TEST-DSN-UNIFY` / `RISK-GOMOD-DIRECT`），证据坐标引 ODR-069 / ODR-072（`tellurion-docs:docs/archive/odr/`）；本轮只立单不修复。此前 2026-11-11 新增 `RISK-CONTRACTS-SNAPSHOT-MIGRATION`（跨仓 ADR-040：`contracts/risk` 为形状唯一真相，本仓改别名引用，`Bundle`/`ParseBundle` 留本仓）；`QT-42` ✅ 订正（2026-11-10：正文「收口记录」早已判 ✅，标题漏改；四条未落项均在 ⑥ 边界外且由 CT-03 / SVC-CORE-RISK-RETIRE / 数值口径裁决 / `risk.limits` 遗留空表裁决关闭，本仓零代码改动）；`SVC-RISK-G3` 收口（2026-11-10：core `risk_manager.*` 读点 14→0、core 新增新旧限额逐字段对拍测试、真 PG + 真 risk 进程的告警回传跑出 `risk.risk_events` 真实行与 `monitor/alerts count:1`、停服后 outbox `failed/attempts=3` 不静默丢；本仓零代码改动）；`SVC-RISK-G4` 收口（2026-11-10：core 侧停服前后各一次真退出码，补齐 ⑤③）；`QT-74` 收口（2026-11-10：迁 contracts 由 CT-03、core 消费由 SVC-CORE-RISK-RETIRE 关掉，数值口径 1:1 零换算，`risk.limits` 遗留空表）；`SVC-RISK-G1` 收口（2026-11-10：Docker 引擎 29.8.0 起来后 `docker compose up --wait` EXIT=0、pg/risk 双 healthy，`/healthz` 200、`/readyz` 200，PG 停→503、恢复→200，翻 ✅）；QT-42 补丁 —— PG Store 审批跃迁修复（`updateRule` 先覆盖后插入）+ 首条真 PG 回归腿 `internal/pgstore/approve_pg_test.go`（2026-11-07，破坏验证真做真还原）；QT-80 施工 —— risk_events.actor 留痕（claims 取值 + 两条用例 + 破坏验证 + 真 PG 两条集成腿，2026-11-07 补齐后 ✅）；SVC-RISK-G4 施工（2026-11-07）—— 失败语义五类 / 超时重试口径 / monitor `configured` 判别位 / runbook / 本仓侧停服演练；此前 SVC-RISK-G0 完成（Dockerfile + deploy/compose + 默认 8089 与 TCP 探针）；**2026-11-10**：本机 PostgreSQL 16.15 @ `127.0.0.1:5432` 上补齐真 PG 腿（`internal/pgstore` 全量用例全 PASS、`go test ./...` 6 包 ok、runbook 的 psql 审计/熔断/表存在三条腿）；Docker/compose 腿随后（同日）补跑通过，见 `SVC-RISK-G1`
completed-2026-11-12: `RISK-GRACEFUL-SHUTDOWN`（`b547110`）/ `RISK-PGDOWN-503`（`d94358e`）/ `RISK-TEST-DSN-UNIFY`（`27a3e48`）/ `RISK-GOMOD-DIRECT`（`4a14b9c`）✅，原文与施工记录在 `docs/archive/tasks-archive-2026-11.md` §三
completed-2026-11-11: `RISK-CONTRACTS-SNAPSHOT-MIGRATION` ✅（快照形状 / 冻结词表 / `DomainSnapshotHash` / 四个跨仓哨兵改 `contracts/risk` 别名 + `SnapshotHash` 委托；`go.mod` pin `v0.10.0`；对拍腿 `TestSnapshotHash_DelegatesToContracts`；原文与施工记录在 `docs/archive/tasks-archive-2026-11.md`）
---

# TASKS — tellurion-risk 未完成项

> **归档说明（2026-11-11）**：本文件已瘦身为「只装未做的事」。2026-11-10 及以前已收口（✅）的任务原文、施工记录、验收输出与破坏验证，已逐字移入 [`docs/archive/tasks-archive-2026-11.md`](archive/tasks-archive-2026-11.md)；该文件顶部有「检索索引」表（ID / 标题 / 收口日期 / commit / 证据链接）。检索：`grep -n "<任务ID或commit>" docs/archive/tasks-archive-2026-11.md`。
>
> 状态：`⬜ 待做` / `🔶 进行中` / `⛔ 阻塞（有未解除前置）` / `⏸ 挂起（外部依赖或已裁决暂不实施；**不计入开放项**，重开须另立单并满足该条「重开条件」）` / `✅ 完成（附日期，随即移入归档）`

## 一、未做 / 挂起 / 外部依赖

> 本节 = 本轮（2026-11-11）台账重整时登记的遗留边界。此前已收口单的正文与施工记录全部在归档文件里。

### RISK-RULE-SEED · 出厂规则集 seed 缺失 [P1] ⏸

- **① 目标**：新部署的空库能拿到一套可用的出厂审批规则，而不是只有本机库里手写的那 4 条。
- **② 上下文**：验收时用的 4 条规则（额度 / 单笔上限 / 白名单等）是直接写进本机 PG 的，**仓库里没有 seed 脚本、迁移或启动注入**；`docs/db-schema.md` 只描述表形状。空库时 `resolve` 返回 `no_approved_rules`，core（`resolve_enabled: true` 出厂）**启动失败**——这是 fail-closed 设计，不是「空集放行」；缺少 seed 的后果是新部署起不来，属可用性问题。
- **③ 要求**：① 出厂规则以**可重放、幂等**的形式入库（seed SQL / 迁移二选一，写进 DDL 播放顺序）；② 规则 hash 与版本号可复现，便于 `contracts/risk` 的对拍；③ 文档写明「空规则集」的语义与告警。
- **④ 约束与非目标**：不改规则表结构与快照 hash 算法（`contracts/risk` 冻结）；不做规则的可视化编辑。
- **⑤ 验收**：空库按标准流程起服务后，规则条数 = 出厂条数且 hash 与固定向量一致；破坏验证 = 不跑 seed ⇒ 启动告警或拒绝进入「已接入」状态。
- **⑥ 边界**：规则真相源归本仓；core 侧只消费快照（见 `tellurion:CORE-RISK-SNAPSHOT-RELOAD`）。
- **重开条件**：任何一次空库部署验收，或规则集需要随版本发布。

### RISK-EVENTS-COVERAGE · 事件回传链路（`risk_service.events_enabled`）未覆盖 [P2] ⏸

- **① 目标**：把「core 打开 `risk_service.events_enabled` → 事件投递到本仓 `/api/risk/events`」这条链真跑一次。
- **② 上下文**：出厂 `events_enabled=false`（`tellurion:config/analysis-service.yaml:211`），本仓 `internal/httpapi/monitor.go` 的 `configured:false` 分支与 `api.go` 的「本仓看不见 core 的开关」注释都说明这条链默认不在场上；`SVC-RISK-G3` 只验证了告警 outbox 的 `pending → sent / failed`，**没有**验证 core 真把事件 POST 过来。
- **③ 要求**：① 真栈打开 `events_enabled=true`，跑一次告警事件从 core outbox 到本仓落库；② 覆盖 401 / 超限 / 重投三种边界；③ 结果写进本节或另立收口记录。
- **④ 约束与非目标**：不改报文形状与状态词表；不引入 MQ。
- **⑤ 验收**：本仓侧能看到 core 投递的真实事件行；破坏验证 = 关掉开关 ⇒ 本仓 `/monitor` 报 `configured:false`。
- **⑥ 边界**：接收面归本仓；投递面归 `tellurion`（`cmd/analysis/wiring/risk_events.go`）。
- **重开条件**：事件回传从「默认关」转为任一部署默认开。

### RISK-CREDENTIAL-PATH · 带凭证路径未取证（`RISK_SERVICE_RESOLVE_TOKEN`）[P1] ⏸

- **① 目标**：本仓在**非回环、带 token** 的形态下真被 core 调通过一次。
- **② 上下文**：`ADR-0005` / `ADR-0006` 已把鉴权与两条服务间链路写死，`QT-77` 的端到端只跑过「回环开放 / 空 token」；`RISK_SERVICE_RESOLVE_TOKEN` 同时非空的生产形态**从未真跑**。core 侧同一缺口登记为 `tellurion:CORE-RISK-CREDENTIAL-PATH`。
- **③ 要求**：① 真 token 起本仓 + core，跑 resolve 200；② 错 token / 缺 token 各跑一次 401；③ 记录真实命令、状态码与日志原文。
- **④ 约束与非目标**：不改策略表与鉴权实现；token 只走环境变量。
- **⑤ 验收**：三条状态码（200 / 401 / 401）；破坏验证 = 换错 token ⇒ 401。
- **⑥ 边界**：本仓校验侧；core 侧装配归 `tellurion`。
- **重开条件**：任何一次真实部署验收。

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

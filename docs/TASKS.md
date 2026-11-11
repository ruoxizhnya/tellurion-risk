---
status: active
last-verified: 2026-11-11
verified-by: 2026-11-11 全面审查 W7 台账立单（A5 授权）——新立 4 条六字段单（`RISK-GRACEFUL-SHUTDOWN` / `RISK-PGDOWN-503` / `RISK-TEST-DSN-UNIFY` / `RISK-GOMOD-DIRECT`），证据坐标引 ODR-069 / ODR-072（`tellurion-docs:docs/archive/odr/`）；本轮只立单不修复。此前 2026-11-11 新增 `RISK-CONTRACTS-SNAPSHOT-MIGRATION`（跨仓 ADR-040：`contracts/risk` 为形状唯一真相，本仓改别名引用，`Bundle`/`ParseBundle` 留本仓）；`QT-42` ✅ 订正（2026-11-10：正文「收口记录」早已判 ✅，标题漏改；四条未落项均在 ⑥ 边界外且由 CT-03 / SVC-CORE-RISK-RETIRE / 数值口径裁决 / `risk.limits` 遗留空表裁决关闭，本仓零代码改动）；`SVC-RISK-G3` 收口（2026-11-10：core `risk_manager.*` 读点 14→0、core 新增新旧限额逐字段对拍测试、真 PG + 真 risk 进程的告警回传跑出 `risk.risk_events` 真实行与 `monitor/alerts count:1`、停服后 outbox `failed/attempts=3` 不静默丢；本仓零代码改动）；`SVC-RISK-G4` 收口（2026-11-10：core 侧停服前后各一次真退出码，补齐 ⑤③）；`QT-74` 收口（2026-11-10：迁 contracts 由 CT-03、core 消费由 SVC-CORE-RISK-RETIRE 关掉，数值口径 1:1 零换算，`risk.limits` 遗留空表）；`SVC-RISK-G1` 收口（2026-11-10：Docker 引擎 29.8.0 起来后 `docker compose up --wait` EXIT=0、pg/risk 双 healthy，`/healthz` 200、`/readyz` 200，PG 停→503、恢复→200，翻 ✅）；QT-42 补丁 —— PG Store 审批跃迁修复（`updateRule` 先覆盖后插入）+ 首条真 PG 回归腿 `internal/pgstore/approve_pg_test.go`（2026-11-07，破坏验证真做真还原）；QT-80 施工 —— risk_events.actor 留痕（claims 取值 + 两条用例 + 破坏验证 + 真 PG 两条集成腿，2026-11-07 补齐后 ✅）；SVC-RISK-G4 施工（2026-11-07）—— 失败语义五类 / 超时重试口径 / monitor `configured` 判别位 / runbook / 本仓侧停服演练；此前 SVC-RISK-G0 完成（Dockerfile + deploy/compose + 默认 8089 与 TCP 探针）；**2026-11-10**：本机 PostgreSQL 16.15 @ `127.0.0.1:5432` 上补齐真 PG 腿（`internal/pgstore` 全量用例全 PASS、`go test ./...` 6 包 ok、runbook 的 psql 审计/熔断/表存在三条腿）；Docker/compose 腿随后（同日）补跑通过，见 `SVC-RISK-G1`
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

### RISK-GRACEFUL-SHUTDOWN · 裸 `ListenAndServe` 无信号处理 [P1] ⬜

- **① 目标**：给 risk-service 优雅停机（SIGTERM 排空在途请求），对齐 core/agent/broker/orch 已有的信号处理。
- **② 上下文**：审查发现 W5-F2（ODR-072）。`cmd/riskservice/main.go:91` 裸 `ListenAndServe` 无 `signal.Notify`；停服时在途的 resolve/events 请求被硬断。本仓 `Store.Tx` 行锁事务在硬断时可能留半完成态。
- **③ 要求**：① `signal.Notify(SIGTERM/SIGINT)` + `server.Shutdown` 排空；② 停机时在途事务有明确收尾（不静默丢）；③ 与 `RISK-PGDOWN-503` 撞 main.go/api.go 按单号串行。
- **④ 约束与非目标**：不改规则/快照语义；不引入 MQ；不改探针口径。
- **⑤ 验收**：SIGTERM 真栈排空（A4 演练，E1）；破坏验证 = 删 Shutdown ⇒ 在途请求硬断用例红。
- **⑥ 边界**：本仓停机；其余三仓同族缺口归各自 `*-GRACEFUL-SHUTDOWN`。
- **owner**：待定

### RISK-PGDOWN-503 · PG 不可达 default 分支回 500 而非 503（D5=a）[P2] ⬜

- **① 目标**：让 PG 不可达时回 **503 `dependency_unavailable`**（可判定、可重试），而不是 default 分支的 500；并消除 api-schema 自相矛盾。
- **② 上下文**：审查发现 W2-F9（ODR-069）+ 裁决 D5=a。`internal/httpapi/api.go:419-420` default 分支 → 500；`docs/api-schema.md:335` 与 `:372` 对 PG-down 状态码口径矛盾（一处 503、一处 500）。D5=a：统一 503 `dependency_unavailable`（对齐 data/analytics 的依赖不可达语义）。
- **③ 要求**：① PG 不可达分支显式 503 `dependency_unavailable`；② api-schema.md:335/:372 口径统一；③ 真栈停 PG → 503、恢复 → 200（同进程自愈）；④ 不把非依赖类错误也吞成 503。
- **④ 约束与非目标**：不改规则/快照形状；不放宽鉴权；503 只用于依赖不可达，业务错误仍按原码。
- **⑤ 验收**：停 PG 真栈 503（E1）；破坏验证 = 改回 500 ⇒ 用例红；api-schema 两处一致。
- **⑥ 边界**：本仓错误语义与 api-schema；core 侧消费超时口径归 `tellurion`。
- **owner**：待定

### RISK-TEST-DSN-UNIFY · 测试 DSN 两个 env 名并存且均未登记 [P2] ⬜

- **① 目标**：把本仓测试 DSN 统一到**一个** env 名，并在 AGENTS §2 / runbook 登记，消除「设了一个名、另一半测试仍 skip」的假绿。
- **② 上下文**：审查发现 W6-F1（ODR-073）。`RISK_TEST_DSN`（`actor_pg_test.go:23` / `migrate_test.go:27` / `monitor_test.go:21`）与 `TELLURION_RISK_TEST_DSN`（`approve_pg_test.go:32`）两个名并存；AGENTS §2 与 runbook 均未登记 ⇒ 设一个名只跑一半真库腿，另一半静默 skip。
- **③ 要求**：① 统一到一个 env 名（写死，其余改别名或同读）；② AGENTS §2 + runbook 登记该名与 `_test` 库要求；③ 无 DSN 仍自动 skip（不假绿），但设了名须全跑。
- **④ 约束与非目标**：不改测试断言；不在活库上跑（只 `_test` 库）；不引入第二套 DSN 解析。
- **⑤ 验收**：设统一名后全部真库腿真跑（数 PASS 行，E1）；破坏验证 = 设旧名 ⇒ 对应用例 skip 可见。
- **⑥ 边界**：本仓测试 DSN 与文档；其余仓 DSN 命名归各仓。
- **owner**：待定

### RISK-GOMOD-DIRECT · contracts 被直接 import 却标 `// indirect` [P3] ⬜

- **① 目标**：把 `go.mod` 里被直接 import 的 `tellurion-contracts` 从 `// indirect` 订正为直接依赖，让依赖表与事实一致。
- **② 上下文**：审查发现 W1-F4（ODR-068）。`go.mod:35` `tellurion-contracts v0.10.0 // indirect`，但 `internal/rule/model.go:9` 直接 import contracts/risk（2026-11-11 复核仍 indirect）。`RISK-CONTRACTS-SNAPSHOT-MIGRATION` 改别名引用后已是直接依赖，标记未跟上。
- **③ 要求**：① `go mod tidy` 让 contracts 转直接依赖（去 indirect）；② AGENTS §4 依赖表已写 v0.10.0，核对一致；③ 不改 pin 版本。
- **④ 约束与非目标**：不升/降 contracts 版本；不加 replace；不顺手 tidy 无关依赖。
- **⑤ 验收**：`go.mod` contracts 无 indirect；`go build/vet/test ./... -count=1` 绿。
- **⑥ 边界**：本仓 go.mod 标记；版本列文档归 `tellurion:X-AGENTS-DEPENDENCY-TABLE`。
- **owner**：待定

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

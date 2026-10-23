---
status: active
last-verified: 2026-10-24
verified-by: QT-42 X3d 熔断指令实装（施工记录已落）
---

# TASKS — tellurion-risk 未完成项

> 本仓**唯一**任务台账。跨仓任务只在 §二 留指针，**不镜像正文**（双写必然漂移）。
> 状态：`⬜ 待做` / `🔶 进行中` / `⛔ 阻塞（有未解除前置）` / `✅ 完成（附日期）`
> 每张单六字段（① 目标 ② 上下文 ③ 要求 ④ 约束与非目标 ⑤ 验收 ⑥ 边界），判据见 `tellurion:AGENTS.md` §8.5。
> 完成即标 ✅（附日期）并移入 `docs/archive/TASKS-YYYY-MM-DD-archived.md`；本文件只留未完成项。

---

## 一、本仓任务

> **约定变更（2026-10-21）**：原「占位仓不写正式六字段任务单」作废——X 线任务单已展开为本仓正式任务（见下）。M5/M8 开工前置不变；实现仍未开工，开工前仍须过前置两条。

### QT-42 · X3 风控服务（规则真相源 + 进程内 RiskPolicy）[P2] 🔶

> **迁入说明（2026-10-21）**：本单自 core 仓 `tellurion:docs/TASKS.md`「五、X 线」迁入（任务台账批量重分布：X 线单展开到各服务仓为正式六字段单，「占位仓不写正式任务单」旧约定同时作废）。六字段正文逐字保留、不改写；core 侧同号条目已改为指针。
>
> **M8 前置完成（2026-10-22）**：接口对表见 [`ARCHITECTURE.md`](ARCHITECTURE.md) §9。开工前先读 §9.1 的五条前置事实——`CheckOrder` **零实现零调用**、`risk_rules_hash` 写入路径已由 QT-63 落库但**恒为空**（等 X3b 填）、`/api/risk/*` 前缀已被 core 四条进程内端点占用、U7 未裁（监控半边形状不冻结）。对表当时只是开工前置、不是实现（2026-10-23 起 X3a 已实装，见下）。
> 对表自检（2026-10-22）：21 条 core `path:line` 按 HEAD `dbe4c68` 逐条命中；C 组 12 端点 / 13 错误码与 `api-schema.md` 双向一致；破坏验证三腿（改错行号 / 删一行端点 / 删一个错误码）各自红，还原后绿。
>
> **首批实装（2026-10-23）**：X3a 规则侧已落（见下方施工记录），本单状态改 🔶；X3b 亦已落（见 X3b 施工记录）；X3c / X3d 与监控半边仍未开工。

- **① 目标**：风控规则可版本化、可审计；回测与实盘调用**同一** RiskPolicy；结果能记 `risk_rules_hash`。
- **② 上下文**：`risk-service-design.md`（D20）；`risk.rule_defs`/`rule_revisions`/`rule_snapshots` 待建；`risk_events` 表未落地（`OrderIntent` 注释已指向）。
- **③ 要求**：① 规则表 + `version_hash`（复用 factor 样板）+ 快照下发 `/api/risk/snapshots/{hash}`；② 进程内 `RiskPolicy` 接口化（现状 `RiskManager` 已无状态，可直接适配）；③ `CheckOrder` **永不走 HTTP**；④ 熔断指令走异步；⑤ 结果记 `risk_rules_hash`（QT-03 分量）。
- **④ 约束与非目标**：不把逐笔裁决搬出进程；不与 compliance/alert 的既有实现重复造；不改 `RiskManagerConfig` 的默认值。
- **⑤ 验收**：锁定 hash 的规则快照可复现；换规则 → `run_spec_hash` 变；双跑对拍一致。
- **⑥ 边界**：只做规则与裁决面；聚合监控/报送属同单的后续切片。

> **施工记录（2026-10-23 · X3a 规则侧首批实装）**
>
> 已交付（本仓 `tellurion-risk`，单仓零跨仓依赖，contracts / kit 均未引入）：
>
> - `internal/rule`：冻结枚举（4 类型 / 4 作用域 / 4 状态）、`Revision.Validate`（JSON 对象 + 拒尾随内容 + 64 KiB 上限）、
>   内容寻址 `version_hash`（域 `tellurion.risk_rule_version_hash.v1`，**不含** description / status / 时间）、
>   `Store` 端口 + `Memory` 实现（全部进出深拷贝）、7 条幂等 DDL（`rule_defs` / `rule_revisions` / `audit_logs` + 2 索引）、
>   `Service`（create 幂等 / update 基准守卫 / approve 一次走完 draft→approved→active 并让旧 active 转 deprecated / 全写审计）。
> - `internal/httpapi`：4 端点（`POST /rules`、`PUT /rules/{id}`、`POST /rules/{id}/approve`、`GET /rules?scope=&active=`），
>   严格解码（`DisallowUnknownFields` + `MaxBytesReader` + 拒多对象）、13 错误码中可达的 8 个、201/200 重放语义。
> - `cmd/riskservice`：`127.0.0.1:8086` 起服务（`Memory` Store）。
> - 测试：`internal/rule`（hash 内容寻址 / fail-closed / 状态机 / 审计顺序 / 深拷贝 / DDL 形状）、
>   `internal/httpapi`（生命周期 / wire code 16 case / 空列表非 null / 405）。
> - 裁决落 `docs/adr/0002-规则版本-hash-与审批状态机的裁决.md`；`docs/db-schema.md` §5、`docs/api-schema.md` 实现状态段、
>   `docs/dataflow.md` 规则侧时序同步更新。
>
> 未落项（本单仍未完成，**不得**按已交付理解）：
>
> 1. **PG Store 与迁移执行入口**：`rule.Migrations()` 已交副本，但没有 PG 实现、没有启动时执行接线（当前只有 `Memory`）。
> 2. **PG 事务边界**：Service 现用进程内互斥锁串行「读-改-写」，接 PG 后必须换事务，否则并发 approve 会丢更新。
> 3. **X3b 快照**：`rule_snapshots` 表、`/snapshots/{hash}`、`POST /resolve`、`/bundle`、进程内 `RiskPolicy` 构建与 `risk_rules_hash` 回填。
> 4. **X3c 事件**：`risk.risk_events` 表与 `POST /events`（异步批量、207 partial_failure）。
> 5. **X3d 熔断**：`risk.circuit_orders` 表与 `POST /breakers`（幂等键）。
> 6. **监控 3 端点**：`monitor/exposure` / `alerts` / `evaluate`——U7（compliance / alert 归属）未裁，形状不冻结，**本切片不做**。
> 7. **鉴权与 actor**：QT-50 ④ 未落，审计 `actor` 恒为空串；`Actor` 字段已用 `json:"-"` 防止调用方自报身份，HTTP 传 `actor` 会被严格解码拒收。
> 8. **`risk.limits` 表**：限额表未建（X3b 之后）。
> 9. **审计读取无 HTTP 端点**：`Store.ListAudit` 只有进程内接口，对外查询未定（避免过早冻结形状）。
> 10. **`rule_deprecated` 错误码不可达**：冻结接口面无 deprecate 端点，实现里不写不可达分支（见 `docs/api-schema.md` §2 注）。

> **施工记录（2026-10-23 · X3b 快照侧）**
>
> 已交付（本仓，单仓零跨仓依赖不变）：
>
> - `internal/rule/snapshot.go`：`Snapshot` / `SnapshotRule` wire 形状；`SnapshotHash`
>   （域 `tellurion.risk_snapshot_hash.v1`，按 rule_id 排序 + canonical params + 长度前缀域，
>   **不含** `created_at`——同一内容永远同一 hash，缓存按 hash 永不失效；未知 type/scope fail-closed 拒收）；
>   `ParseBundle`（bytes → `Bundle` 纯函数：DisallowUnknownFields 严格解码 + 重新 derive hash 比对，
>   不符即 `ErrSnapshotMismatch` fail-closed 拒开跑）+ `Bundle.ForScope`（找不到 scope = 没有规则，不给默认限额）。
>   **这是进程内 RiskPolicy 的输入边界**：零 I/O 零 HTTP，core 侧构建 `RiskPolicy` 消费它。
> - `internal/rule`：`rule_snapshots` DDL（PK hash + 64-hex CHECK + 非空数组 CHECK，`migrationStatements` 共 8 条）；
>   `Store` 端口 + `Memory` 增加 `PutSnapshot` / `GetSnapshot`（**双头 derive 校验**：写入与读取都重算比对；
>   同 hash 重复写 = no-op，同 hash 不同内容 = fail-loud，不许静默覆盖）；哨兵 `ErrSnapshotNotFound` /
>   `ErrNoApprovedRules` / `ErrSnapshotMismatch`；`Service.Resolve`（hash / latest 二选一，latest 把当前
>   **active revisions** 收进快照并按内容寻址幂等落库；draft 永不进快照；无生效规则 → `ErrNoApprovedRules`）。
> - `internal/httpapi`：+3 端点（`POST /resolve`、`GET /snapshots/{hash}`、`GET /snapshots/{hash}/bundle`，
>   bundle 与快照本体同一 canonical JSON）+ wire code 映射（404 `snapshot_not_found` / 404 `no_approved_rules`）。
> - 测试：hash 内容寻址（排序无关 / params 书写无关 / 逐字段变更换 hash）、fail-closed（重复 rule_id /
>   未知类型 / 空快照）、resolve 只有 active 进快照（draft head 不进）、无审批拒开跑、钉 hash 不回退 latest
>   （历史快照仍可复现）、写/读双侧篡改检测、`ParseBundle` 回环 + scope 查找 + 篡改拒收；
>   wire 级同一 hash 两次拉取**逐字节一致**（dataflow 不变量 1/2/3 全部有测试名）。
> - 文档同笔：`db-schema.md` §1/§1约束/§5、`api-schema.md` 头部/§1/§2、`dataflow.md` 主链路/run 前时序/失败表/不变量、
>   `ARCHITECTURE.md` §3.1/§8/§9.3/§9.4/§9.5、`AGENTS.md` §1。
>
> 验收口径对照（本单 ⑤）：「锁定 hash 的规则快照可复现」= ✅（`TestResolve_PinnedHashNeverFallsBack` +
> wire 字节一致）；「换规则 → hash 变」= ✅（快照级内容寻址测试）；「双跑对拍一致」= ⛔ core 侧接线未做。
>
> 未落项（在原 10 条之上更新；原 3 条改写，其余不变）：
>
> 3'. **core 侧消费接线（原「X3b 快照」剩余半边）**：`risk_rules_hash` 生产者仍写空串（F3，QT-63 落的表）——
>   core 要在 run 前调 `resolve`、拿 bundle 进 `ParseBundle` 构建 `RiskPolicy`、把 hash 写进 `run_provenance`。
>   这是跨仓单（core 侧改动），不在本仓。`ParseBundle` 的 Go 类型出现第二个使用方（core）后，
>   `Snapshot` / `SnapshotRule` 按 ADR-0001 ① 迁 contracts。

> **施工记录（2026-10-24 · PG Store + 迁移执行 + 事务边界）**
>
> 已交付（本仓，单仓可自证）：
>
> - `internal/rule`：`Store.Tx(ctx, fn)` 端口；`Service` 的 `Create` / `Update` / `Approve` / `Resolve(latest)`
>   全部在 `Tx` 内完成「读-改-写」，**删除进程内互斥锁**（ADR-0003）；`Memory.Tx` 用全量状态深拷贝 +
>   失败还原模拟回滚；`Resolve(latest)` 落库后回读快照，同 hash 的 `created_at` 不漂移。
> - `internal/rule/ddl.go`：`risk.limits` 同批建表（scope + subject + limit_key；仅建表，读写路径未冻结）。
> - `internal/pgstore`：`rule.Store` 全量 PG 实现（`Tx` = BEGIN/COMMIT/ROLLBACK；事务内 `GetDefinition`
>   用 `SELECT ... FOR UPDATE`；JSONB / TIMESTAMPTZ / NULL 映射；唯一约束冲突翻译为领域哨兵）；
>   `Migrate` 按序执行 `rule.Migrations()`。
> - `cmd/riskservice`：`-dsn` / `RISK_DATABASE_URL` 非空 → Ping → Migrate → PG Store；
>   空 DSN 显式告警后退回 Memory（仅本地开发）。
> - 依赖登记：`github.com/jackc/pgx/v5 v5.7.5`（AGENTS §4；仅进程入口空白导入 `stdlib`）。
> - 测试：Memory Tx 全量回滚、Approve 中途写失败整笔回滚、PG 唯一约束错误映射、`risk.limits` DDL 形状。
> - 文档同笔：`db-schema.md` §1/§4/§5/§6、`api-schema.md` 头部、`dataflow.md` 现状/失败表/不变量、
>   `ARCHITECTURE.md` §3.1/§5/§6/§8/§9.5、`AGENTS.md` §1/§4、ADR-0003 + ADR 索引。
>
> 验收口径：`go build ./... && go vet ./... && go test ./... -count=1` 全绿；破坏验证（把 Approve 的
> `Store.Tx` 改回直接调用 Store）→ `TestServiceApprove_RollsBackAllWritesOnFailure` 红，逐字节还原后绿。
>
> 未落项（本条**取代** X3a 记录里的第 1、2 条；其余继续有效，另加一条）：
>
> 1. ~~PG Store 与迁移执行入口~~ ✅ 2026-10-24。
> 2. ~~PG 事务边界~~ ✅ 2026-10-24（ADR-0003）。
> 3. X3c 事件：`risk.risk_events` 表与 `POST /events`（异步批量、207 partial_failure）。
> 4. X3d 熔断：`risk.circuit_orders` 表与 `POST /breakers`（幂等键）。
> 5. 监控 3 端点：`monitor/exposure` / `alerts` / `evaluate`——U7 未裁，形状不冻结，本切片不做。
> 6. 鉴权与 actor：QT-50 ④ 未落，审计 `actor` 恒为空串。
> 7. `risk.limits` 的读写路径与端点：表已建，读取口径（快照附带 / 独立端点）未冻结。
> 8. 审计读取无 HTTP 端点：`Store.ListAudit` 只有进程内接口。
> 9. `rule_deprecated` 错误码不可达（冻结接口面无 deprecate 端点）。
> 10. **core 侧消费接线（跨仓）**：`risk_rules_hash` 生产者仍写空串（F3）；需 core 在 run 前
>    `resolve` → `ParseBundle` → 写 `run_provenance`。不在本仓施工，已报协调者。

> **施工记录（2026-10-24 · X3c 事件回传）**
>
> 已交付（本仓）：
>
> - `internal/event`：wire `Event`（`run_id` / `order_id` / `event_seq` / `symbol?` / `verdict?` /
>   `reason?` / `payload?` / `ts`）、`Validate` fail-closed、去重键 `(run_id, order_id, event_seq)`、
>   `Memory` Store、`Service.Ingest`（逐条校验；单条不合法记 `Failure{index, code, message}` 后继续；
>   未知字段不丢，落库原文进 `Record.Raw`）。空批 / 整批不可解析 → `ErrInvalidParams`。
> - `risk.risk_events` DDL（主键 = 去重键；`raw JSONB NOT NULL` 保留原文；`payload JSONB`；
>   `event_ts` / `received_at`；`CHECK (event_seq >= 0)`）。
> - `internal/pgstore`：`event.Store.PutEvents`（单事务逐行 `INSERT ... ON CONFLICT DO NOTHING`；
>   `accepted` / `duplicates` 计数；任何错误整批回滚）。
> - `internal/httpapi`：`POST /api/risk/events`（`WithEvents` 按需装配）：200 `{accepted, duplicates}`；
>   207 `{accepted, duplicates, failed[], code:"partial_failure"}`；400 `invalid_params`；落库失败 500。
> - `cmd/riskservice`：事件服务装配（PG 与规则共用 `*pgstore.Store`，Memory 模式用 `event.NewMemory`）。
> - 测试：去重（含同 run/seq、不同 order_id 必须接受）、重放、部分失败 + 未知字段保留、空批、
>   wire 200/207/400/405。
> - 文档同笔：`db-schema.md` §1/§1约束/§5/§6、`api-schema.md` 头部/§1/§2/§3、`dataflow.md` 主链路/
>   run 后/失败表/不变量、`ARCHITECTURE.md` §3.1/§8/§9.3/§9.4/§9.5、`AGENTS.md` §1。
>
> 验收口径：`go build ./... && go vet ./... && go test ./... -count=1` 全绿；破坏验证（`dedupeKey`
> 去掉 `order_id`）→ `TestIngest_DedupesByRunOrderSeq` 红，逐字节还原后绿。
>
> 未落项（本条**取代**上一条列表；X3c 只落事件回传半边）：
>
> 1. **X3d 熔断**：`risk.circuit_orders` 表与 `POST /breakers`（幂等键）。
> 2. **监控 3 端点**：`monitor/exposure` / `alerts` / `evaluate`——U7 未裁，形状不冻结，本切片不做。
> 3. 鉴权与 actor：QT-50 ④ 未落，审计 `actor` 恒为空串。
> 4. `risk.limits` 的读写路径与端点：表已建，读取口径未冻结。
> 5. 审计读取无 HTTP 端点：`Store.ListAudit` 只有进程内接口。
> 6. `rule_deprecated` 错误码不可达（冻结接口面无 deprecate 端点）。
> 7. **core 侧消费接线（跨仓）**：`risk_rules_hash` 生产者仍写空串（F3）；不在本仓施工，已报协调者。

> **施工记录（2026-10-24 · X3d 熔断指令）**
>
> 已交付（本仓）：
>
> - `internal/breaker`：wire `Command`（`breaker_id` / `action` / `scope` / `reason?` / `ts`）、
>   冻结动作枚举 `halt` / `cancel_all`、`Validate` fail-closed（未知 action / scope 拒收）、
>   幂等载荷身份 `SamePayload` = `action + scope + reason`（`ts` 不参与，重试可换时间）、
>   `Memory` Store、`Service.Apply`。
> - `risk.circuit_orders` DDL（`breaker_id` 主键；`action` / `scope` CHECK 与 Go 枚举同源；
>   `status` 今天只写 `applied`；`command_ts` 与 `received_at` 分开存）。
> - `internal/pgstore`：`breaker.Store.PutBreaker`（事务内 `INSERT ... ON CONFLICT DO NOTHING`；
>   冲突时读回并比对载荷身份：相同 → already_applied，不同 → `ErrIdempotencyConflict`）。
> - `internal/httpapi`：`POST /api/risk/breakers`（`WithBreakers` 按需装配）：
>   200 `{breaker_id, status:"applied"|"already_applied"}`；409 `idempotency_conflict`；
>   400 `invalid_params`（未知 action / scope / 缺字段）。
> - `cmd/riskservice`：熔断服务装配（PG 与规则/事件共用 `*pgstore.Store`，Memory 模式用 `breaker.NewMemory`）。
> - 测试：首次 applied、同键同载荷（不同 ts）already_applied、同键不同 action/reason → 409、
>   未知 action/scope/缺字段 → invalid_params、wire 200/409/400/405。
> - 文档同笔：`db-schema.md` §1/§1约束/§5/§6、`api-schema.md` 头部/§1/§2/§3/§4、`dataflow.md`
>   run 后/失败表/不变量、`ARCHITECTURE.md` §3.1/§8/§9.3/§9.4/§9.5、`AGENTS.md` §1。
>
> 验收口径：`go build ./... && go vet ./... && go test ./... -count=1` 全绿；破坏验证（`SamePayload`
> 把 `ts` 算进载荷身份）→ 幂等测试与 wire 重放测试红，逐字节还原后绿。
>
> 未落项（本条**取代**上一条列表）：
>
> 1. **监控 3 端点**：`monitor/exposure` / `alerts` / `evaluate`——U7 未裁，形状不冻结，本切片不做。
> 2. **熔断执行方与 ACK**：`circuit_orders.status` 今天只写 `applied`；谁执行、如何回报
>    `acknowledged/failed` 随升格 ADR。
> 3. 鉴权与 actor：QT-50 ④ 未落，审计 `actor` 恒为空串。
> 4. `risk.limits` 的读写路径与端点：表已建，读取口径未冻结。
> 5. 审计读取无 HTTP 端点：`Store.ListAudit` 只有进程内接口。
> 6. `rule_deprecated` 错误码不可达（冻结接口面无 deprecate 端点）。
> 7. **core 侧消费接线（跨仓）**：`risk_rules_hash` 生产者仍写空串（F3）；不在本仓施工，已报协调者。


---

> **开工前置（refactor-plan-review M5/M8，2026-10-20 登记；2026-10-22 更新执行状态）**：本表任何单开工前，必须先过 core `tellurion:docs/TASKS.md`「五、X 线」的**公共前置**两条：
> ① **M5 已结案（2026-10-21）** → `tellurion:docs/design/qt-decoupling/repo-inventory.md` §1.2：**六仓 ≠ 六进程**，本仓今天只到「仓内实现 + core 侧 port」，不升格容器；U7（compliance / alert 归属）仍待裁。
> ② **M8 接口对表已落（2026-10-22）** → 本仓 [`ARCHITECTURE.md`](ARCHITECTURE.md) §9（模板 = `tellurion:docs/design/qt-decoupling/data-service-contract-and-ownership.md` §4.1；**本行原指的 `target-module-topology.md` UnifiedDataPoint 节是幻影引用，已订正**）。
> 正文以 core 彼处为准，此处只留指针、不镜像。

## 施工序（候选单，开工前必须展开成六字段）

> 这张表是**施工顺序**，不是任务台账：每条在执行前要展开成六字段单（目标/上下文/要求/约束/验收/边界），并写明冻结契约的路径与节号。
> 顺序有依赖，**不要跳级**：跳序的代价是返工（例：X1a 的协议没冻结就写 aggregator，等于把协议写死在一个服务里）。
> 2026-10-21：候选单已展开为「一、本仓任务」的正式六字段条目（表中「上游单号」指向的 QT 单正文现在就在本仓）。

| 阶段 | 内容 | 上游单号 | 前置 |
|---|---|---|---|
| X3a | 规则定义 + revision + 审批状态机（draft→approved→active→deprecated） | `QT-42` | 🔶 已实装（`internal/rule` + `internal/httpapi` 4 端点；PG Store / 迁移执行 / 事务边界已落）；鉴权未落 |
| X3b | 快照内容寻址 + `resolve` / `bundle` | `QT-42` | 🔶 已实装（2026-10-23：`rule_snapshots` DDL + 三端点 + `ParseBundle` 边界；Memory/PG 双实现）；core 侧消费接线（F3）未落 |
| X3c | 事件回传（异步批量）+ 监控聚合（敞口 / 告警） | `QT-42` | 🔶 事件回传已实装（2026-10-24：`risk.risk_events` + `POST /events`，200/207 + 去重键）；监控聚合 ⛔ U7 未裁 |
| X3d | 熔断指令（幂等键） | `QT-42` | 🔶 已实装（2026-10-24：`risk.circuit_orders` + `POST /breakers`，幂等键 + 409）；执行方 / ACK 未落 |

## 二、跨仓指针（不在本仓，勿双写）

| 单号 | 主题 | 在哪 |
|---|---|---|
| QT-42 | X3 风控服务（规则真相源 + 进程内 RiskPolicy） | 已迁入本仓「一、本仓任务」（2026-10-21；core `tellurion:docs/TASKS.md` 留同号指针） |
| QT-08 | 本仓 CI / AGENTS / 台账由该单一并补齐 | `tellurion:docs/TASKS.md` |

---

## 维护本文件

- **新增单**：写全六字段；六字段里必须显式引用**冻结契约的路径与节号**（契约仓的包路径、设计文档的节号），不许写"参考相关实现"。
- **派工前先证伪前提**：读码核对工单写的前提；不符就停下回报 + 给拆法，不硬做、不扩范围。
- **跨仓引用写 `repo:path`**（如 `tellurion:docs/TASKS.md`），不做可点击相对链接。
- **顺序纪律**：同一波次内撞同一批文件的单按单号串行，一单一 commit。

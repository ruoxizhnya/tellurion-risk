---
status: active
last-verified: 2026-11-11
verified-by: 2026-11-11 文档对齐审计 —— `SVC-CORE-RISK-RETIRE`（2026-11-09）后 core 消费走 `contracts/risk` + `risk.PolicyFromSnapshot`（不经 `ParseBundle`）、`risk_manager.*` 已删、`resolve_enabled` 出厂 `true`、scope 选择实测（global → strategy）、`risk_events.actor` 已落；此前 QT-74 参数字典口径
---

# Dataflow — tellurion-risk

> 目的：把「数据从哪来、在谁手里变形、到哪去」写死。**任何跨仓数据交换都必须能在本图上找到一条边**；找不到 = 这段代码在偷偷建第二条路。

## 1. 主链路

```
人 / 审批
   │  ① create → ② update（新 revision）→ ③ approve（activate）
   ▼
tellurion-risk  ── 写 risk.rule_defs / risk.rule_revisions / risk.audit_logs / risk.rule_snapshots ──►  （schema 唯一 owner）
   │
   │  ④ resolve / bundle（run 前 1 次）            ✅ X3b（快照内容寻址 + ParseBundle）
   ▼
core 进程内 RiskPolicy  ◄── 每笔 CheckOrder（不走网络；`CheckOrder` 至今仍是 stub）  ✅ 2026-11-09：
   │                                                        ✅ QT-66 —— run 提交时调 resolve 取 hash
   │                                                           写进 `quant.run_provenance`（F3 的 hash 半边关闭）
   │                                                        ✅ 限额装配 —— resolve 内联 `snapshot`
   │                                                           → `contracts/risk.Verify()` + `risk.PolicyFromSnapshot`
   │                                                           （**不经**本仓 `ParseBundle`；`risk_manager.*` 已删）
   └── 异步事件回传 ──►  POST /api/risk/events ──►  risk.risk_events   ✅ X3c（批量 + 去重键 + 207）
                                                          │
                                                          └──► monitor/{exposure,alerts,evaluate}
                                                               只读聚合 / 报表   ✅（2026-10-24，ADR-0004）
```

**现状（2026-11-11 订正）**：左上半段（规则真相源 ①~③）、快照读侧（④ resolve / snapshots / bundle）、
事件回传（批量 + 去重键 + 207）与监控三端点（只读聚合 / 报表）已实现，持久化有 Memory 与 PG（事务边界见 ADR-0003）；
**告警生产侧接线（core `alertLoop` → `POST /events`）已于 2026-10-25 落地**（QT-42 X3c core 腿）。
**F3 两半都已关闭**（2026-11-09 `SVC-CORE-RISK-RETIRE`）：hash 半边（QT-66 —— run 提交时调
`POST /api/risk/resolve`，把 `risk_rules_hash` 写进 `quant.run_provenance`，空串只剩 `not_configured` 一种合法含义）；
兜底半边 —— core 走 `resolve` 的**内联 `snapshot`**（不是 `GET .../bundle`）→ `contracts/risk.Verify()` 重新 derive hash
→ `risk.PolicyFromSnapshot` 构建进程内 Policy；`config/analysis-service.yaml` 的 `risk_manager.*` 已整段删除。
`risk_events` 今天的**唯一生产者是告警流**：
`verdict:"alert"`、`run_id="core:alert-loop"`、`order_id=Alert.ID`、`event_seq=0`，落点
`core:cmd/analysis/wiring/risk_events.go`，开关 `risk_service.events_enabled`（**默认关** —— 关着的部署里
`alerts` / `evaluate` 空集是设定，不是缺陷；G4 起响应带 `configured:false` 可判别，不再靠猜）。仍待：熔断执行方、
run 后逐笔裁决事件回传、`POST /events` 的审计留痕（`risk_events.actor` 已由 QT-80 落列并写入，仍不写 `risk.audit_logs`）、
外部推送通道。`bundle` 端点今天零消费方（core 用 resolve 内联快照），保留为仓内边界与将来消费方备用。
**投递形态已于 2026-10-25 二批改为持久 outbox**（core 的 `quant.alert_outbox` +
后台 relay 重投），本仓那句「允许延迟、不允许丢」不再只是端点侧的承诺 —— 逐条限制见下方
「事件回传失败」行与 `docs/TASKS.md`。

**服务间凭证（2026-10-25，裁-17 + ADR-0006）**：图里 core → 本仓的两条边
（`POST /api/risk/resolve` 与 `POST /api/risk/events`）是**机器发起**的，鉴权上走
`service` 角色（与 `trader` / `admin` 并列写在同一条 `Roles` 里；kit 角色 any-of 且不排序，
两者不能互相顶替）。`GET /api/risk/audit` 与规则管理面**不接** `service`。
本仓只**认**这个角色，不签发、不存、不分发它——签发与注入属 core QT-71 / kit KT-09。

## 2. 关键时序

### 规则变更（已实现）

1. `POST /api/risk/rules`：校验冻结枚举与 params（JSON 对象、≤64 KiB、拒尾随内容 →
   再按 `type` 对参数字典逐项校验键集合与取值，见 `db-schema.md` §7）→ 规范化 →
   `version_hash = H(域, rule_id, revision=1, scope, type, params)` → 落 `rule_defs(head=1, active=NULL, status=draft)`
   + `rule_revisions(revision=1, status=draft, approved_at=NULL)` → 追加 `create` 审计。
   同一 `rule_id` 同内容重放返回 200 与同 hash，**不**再生成 revision；同 id 不同内容 → 409 `idempotency_conflict`。
2. `PUT /api/risk/rules/{id}`：`base_revision` 必须等于当前 `head_revision`（否则 409 `revision_conflict`）→
   `revision = head + 1`，`scope` / `type` 继承，`description` 缺省继承、显式空串才清空 →
   新行 `status=draft`，`active_revision` **不动**（runtime 仍拿旧生效版本）→ 追加 `update` 审计。
3. `POST /api/risk/rules/{id}/approve`：一次动作走完 `draft → approved → active`
   （DDL `CHECK ((status IN ('approved','active','deprecated')) = (approved_at IS NOT NULL))` 恒成立）→
   若存在旧的 active revision：先置 `deprecated` 并追加 `deprecate` 审计，再写新 active → 挪 `active_revision` →
   追加 `approve` 审计。重放已 active 的同一 revision 幂等返回。
4. `GET /api/risk/rules`：只读，不产生版本；列表项 `status` 取 head revision 的状态，`active_revision` 取定义指针。

### run 前（粗粒度，一次）—— ✅ X3b（快照侧）；core 侧接线已落（2026-11-09）

5. 调用方 `POST /api/risk/resolve`（`{"hash": ...}` **或** `{"ruleset": "latest"}`，二选一；
   都给 / 都不给 / ruleset 非 latest → 400 `invalid_params`）
   —— core 代机器调用这一步用 `service` 角色（ADR-0006）；真人调用方用 `trader` / `admin`。
6. latest：服务把**当前 active revisions**收进快照，`risk_rules_hash = H(域, 排序后的 active 内容)`
   （不含 `created_at`，同一内容永远同一 hash）→ 按 hash 幂等落 `rule_snapshots`；
   没有任何生效规则 → 404 `no_approved_rules`（拒绝启动，不许用默认限额）
7. 钉 hash：按 hash 取历史快照；不存在 → 404 `snapshot_not_found`，**不回退 latest**
   （拿不到就拒绝开跑，不许用「现在生效的」顶替「当初锁定的」）
8. core 实际走 `POST /api/risk/resolve` 的**内联 `snapshot`**（不是 `GET .../bundle`）→
   `contracts/risk.Verify()` 重新 derive hash 比对（不符 fail-closed；`DisallowUnknownFields` 拒未知顶层字段）+ 未知 type/scope 拒收
   → `risk.PolicyFromSnapshot` 构建进程内 `Policy`。**不再经过本仓 `rule.ParseBundle`**（它仍是仓内边界，
   `bundle` 端点今天零消费方）。失败口径：`resolve_enabled: true`（出厂默认）时取不到即**启动失败**，
   不回落 `risk_manager.*`（该配置块已删）；只有部署显式 `false` 才用 `risk.BuiltinPolicy()` 并打 Warn。
9. 该 hash 写入本次 run 的结果（可复现锚点）——**已落**（2026-10-25，QT-66：core 每次 run 提交时调
   `POST /api/risk/resolve` 取 hash 写进 `quant.run_provenance.risk_rules_hash`；未接入记空串 +
   `not_configured`，取不到即 run `failed` + `risk_rules_unavailable`）

### scope 消费口径（2026-11-11 实测 core `pkg/risk/policy.go`；QT-74 ④ 原口径为只消费 global）

**core 的选择顺序：优先 `scope = global`；没有 global 规则时退回 `strategy`**；`account` / `symbol`
不参与进程级策略源（`pickScope` 两者皆无 ⇒ 拒绝装配）。选中 scope 内**四类规则各恰好一条**——
缺一类或同类多条 ⇒ 拒（歧义不猜），每类 params 按冻结键名严格解码，未知键 ⇒ 拒。
`account` / `strategy` / `symbol` 三档规则照样进快照（`Bundle.ForScope` 里可见、`bundle` 字节里也真实存在）。
这是**显式口径，不是静默忽略**：回测没有账户身份是事实（不是缺陷），但「下发了却没用」必须看得见。
未参与裁决的档位**不**做任何默认值兜底（没有 global / strategy 规则 = 拒绝开跑）。
> **契约（2026-11-11 主任裁决，正式口径）**：`global → strategy` 回退是**正式契约**——global 提供组合级基线，
> strategy 提供单策略覆盖；有专属规则就用专属、没有就回落，这是正常优先级语义。`QT-74 ④` 的「只消费 global」
> 只描述当次实现范围，**不是设计约束**。契约要求：① 选中 scope 内**四类规则各恰好一条**（缺一类或同类多条 ⇒ 拒，歧义不猜）；
> ② 无 global 且无 strategy ⇒ **fail-closed 拒开跑**；③ `account` / `symbol` 仍不参与进程级策略源；④ 每类 params 严格解码，
> 未知键即拒。`contracts/risk` 侧的同步说明归 contracts 仓（移交清单第 1 项），本仓不改契约仓。

### run 中（热路径，零网络）—— 边界已备，core 接线未做

10. 每笔 `CheckOrder(order)` → 进程内裁决 → 通过 / 拒绝 / 降规模
11. 事件进本地队列

### 鉴权闸（所有端点，同步；2026-10-25）

0. 每个请求先过 `requireAuth`：未装配 `Authorizer` → `503 auth_unavailable`；有 secret 时按 kit
   `auth/httpadapter` 验 `Bearer` access token（拒 refresh / 坏签名 / 过期）→ 401，角色不在策略表 → 403；
   通过后把 `Principal` 放进 context，审计 `actor` 从这里取（open-access 记 `open-access`）。
   策略表（读 viewer / 写 trader / 审批 admin）与路由表由守卫测试双向对拍。
   **服务间档（裁-17 + ADR-0006）**：`POST /api/risk/resolve` 与 `POST /api/risk/events` 两条
   core 代机器发起的链路额外接受 `service` 角色（与 `trader` / `admin` 并列；kit 角色 any-of
   且不排序 ⇒ 不能互相顶替）。`GET /api/risk/audit` 与规则管理面**不接** `service`——
   审计与审批是人负责的动作，机器凭证不代劳。

### run 后（异步）—— ✅ X3c / X3d / 监控（只读）

12. 批量 `POST /api/risk/events` → 逐条校验（缺 `run_id` / `order_id` / `ts` 记 failed）→
    合法条目按 `(run_id, order_id, event_seq)` 幂等落 `risk_events`：已存在 = duplicate（200 计数），
    单条不合法 = 207 `partial_failure` + `failed[{index, code, message}]`；整批落库失败 = 500，重试整批
    —— core 代机器回传这一步用 `service` 角色（ADR-0006）。
13. `POST /api/risk/breakers` 可幂等下发熔断指令（`breaker_id` + 载荷身份 action/scope/reason）：
    首次 `applied`、同键同载荷 `already_applied`、同键不同载荷 409；落 `circuit_orders`。

### 监控读取（同步，只读；2026-10-24）

14. `GET /api/risk/monitor/exposure`：按 `event_ts` 窗口读 `risk_events`，把 `payload`
    投影成名义金额与方向，按 `symbol|account|strategy` 分桶；缺 `notional` / `qty+price` 计入
    `unpriced_events`，方向推不出计入 `unsigned_events`；`verdict=alert` 的事件不参与。
15. `GET /api/risk/monitor/alerts`：只读 `verdict='alert'` 的事件，按 `payload`
    投影成告警（`rule` / `severity` / `message` 必填），按时间倒序返回；非法投影计入 `invalid_events`。
16. `POST /api/risk/monitor/evaluate`：同一投影上按 `min_severity` / `rule` / 窗口过滤并汇总；
    **不运行检测器、不写库、不推送外部通道**；响应 `detection:"outside_process"`。
    三个端点都先过 `Authorizer` 闸：未装配 → 503，无凭证 → 401，越权 → 403。
    三个响应都带 `configured`（G4）：部署声明为 false ⇒ 空集是「未接入」；true + 空 ⇒ 「没触发」。

## 3. 失败模式（每一处 fail-closed 都要能指名）

| 失败点 | 症状 | 兜底 | 现状 |
|---|---|---|---|
| 规则参数不是 JSON 对象 / 含尾随内容 / 超 64 KiB | 规则语义不可寻址 | 400 `invalid_params`，**不落库、不推进 head** | ✅ |
| 规则参数键不在词典 / 取值越域（如权重写成 `10` 百分数形态） | 消费方只能猜字段名与单位（算错钱） | 写侧 400 `invalid_params`（不落库、不推进 head）；`ParseBundle` 对未知键 fail-closed | ✅（`internal/rule/params.go`；`TestParamsDictionary_IsPinned` / `TestValidate_RejectsParamsOutsideDictionary` / `TestParseBundle_RejectsUnknownParamKey`） |
| `version_hash` 与内容不一致（Store 层） | 内容寻址被破坏 | `ErrRevisionConflict`，拒绝写入；`UNIQUE (rule_id, version_hash)` 兜底 | ✅ |
| 编辑基准过期 | 覆盖别人的修改 | 409 `revision_conflict`，要求重读 head | ✅ |
| 未审批规则被当成生效版本 | 生产用上草稿 | 只有 `active_revision` 可达 runtime；`draft` 永不进快照 | ✅（状态机 + DDL CHECK） |
| 并发写同一 rule_id | 丢更新 / 双 active | Service 的读-改-写全部在 `Store.Tx` 内：PG 用 `SELECT ... FOR UPDATE` 行锁，Memory 用全量快照回滚；失败整笔回滚 | ✅（ADR-0003；`TestServiceApprove_RollsBackAllWritesOnFailure` / `TestMemoryTx_RollsBackAllWrites`） |
| 快照 hash 与内容不符（写入 / 读取 / ParseBundle） | 内容寻址被破坏（缓存串号 / 内容被改） | `ErrSnapshotMismatch`，500 `internal` + 告警日志，fail-loud 不吞 | ✅（三处同检：`store.go` 写/读 + `ParseBundle`） |
| 服务不可用 | 拿不到规则快照 | **拒绝开跑**（不许用默认限额） | ✅（resolve 侧：`no_approved_rules` / `snapshot_not_found` 拒绝；core 已接（2026-11-09），`resolve_enabled: true` 时启动期取不到即失败） |
| 规则被改但 run 里没记 hash | 结果不可复现 | `risk_rules_hash` 必写进结果 | ✅（2026-10-25，QT-66 的 core 腿：`core:cmd/analysis/wiring/risk_rules.go` 每次 run 取一次并写进 `quant.run_provenance`；未接入 = 空串 + `not_configured`，取不到 = run `failed`） |
| 事件回传失败 | 留痕缺失 | 批量原子落库：整批失败 500 后重试整批（去重键幂等）；单条不合法 207 只重发 failed；允许延迟，不允许丢 | ✅ 两侧都兑现（2026-10-25 二批）：端点侧 `internal/event` + `risk.risk_events` 主键去重；生产侧 core 的 sink 改成**持久 outbox**（`quant.alert_outbox`，core Migration 060）——告警先落一行，relay 每 `events_relay_interval` 取 pending 重投，触 `events_max_attempts` 转 `failed` 终态**但保留行**，不再有「队列满即丢 / 重启即丢」。**两条残留如实登记**：① `alert_id` 撞键（同秒重启能造出同 id 的两条告警）的那条不落新行，core 侧转 Warn 点名，若既有行已是 `failed` 则这条到不了本仓；② 「core 真进程 → risk 真进程 → `monitor/alerts` 读出非空」的端到端腿仍未跑过（core 侧用 `httptest` + 内存替身按 wire 逐键自证）。见 `tellurion:docs/TASKS.md` QT-42 X3c core 腿二批 |
| 熔断指令重复 | 重复平仓 | 幂等键 `breaker_id`：同键同载荷 → 200 `already_applied`；同键不同载荷 → 409（不执行） | ✅（X3d：`internal/breaker` + `circuit_orders` 主键） |
| 鉴权闸未装配 / 角色不足 | 规则与审计被匿名读写 | 所有端点统一 fail-closed：未装配 `Authorizer` → 503，无凭证 → 401，越权 → 403 | ✅（`httpapi.requireAuth`；`TestAuth_ThreeStatesAndActor` / `TestRouteTableEqualsPolicyTable`） |
| core 调本仓没带 `service` 凭证 | run 前取快照失败 ⇒ 按裁-11 该 run `failed` + `risk_rules_unavailable` | 策略表对 `resolve` / `events` 并列开 `service`（与 `trader` / `admin` 同在一条 `Roles`）；`audit` 与管理面不开 | ✅（2026-10-25 裁-17 + ADR-0006；`TestPolicy_ServiceRoleScope`）。**签发与注入不在本仓**（core QT-71 / kit KT-09） |
| 监控读不到事件（读取端口未装配 / 库错误） | 把「读不到」显示成「没有敞口 / 没有告警」 | `503 dependency_unavailable` 或 500；绝不返回空报告冒充成功 | ✅（`event.ErrMonitorUnavailable`；`TestMonitor_RequiresReader` / G4 的 `TestMonitor_PGUnreachableIsNotEmptySet`） |
| 空集不可判别（未接入 vs 没触发） | 把「core 没接」读成「没触发」或反向 | 三个 monitor 响应都带 `configured` 判别位（部署声明 `RISK_EVENTS_EXPECTED`，缺省 false）；`false` = 未接入，`true`+空 = 没触发 | ✅ G4（`TestMonitor_EmptySetIsDiscriminable`；契约见 `api-schema.md` §1「空集的两种含义」） |
| 本仓停服 / 不可达（core 两条腿） | 裁决链断 | `resolve` 腿：连接错误 / 超时 / 非 2xx ⇒ 启动/run **failed**，**不重试、不回落 `risk_manager.*`**（已删；`resolve_enabled=false` 才走 builtin 并打 Warn）；`events` 腿：连接错误 ⇒ 持久 outbox 重投（去重键幂等），不阻塞 run | ✅ 本仓侧已证（`TestDrill_StoppedServiceFailsLoud`）；core 侧演练已执行（2026-11-10，`SVC-RISK-G4` 收口：停服前后各一次真退出码） |
| 敞口字段缺口 / 窗口过大 | 低报风险 | 缺字段计入 `unpriced_events` / `unsigned_events`（不补零）；窗口 > 5000 条事件 → 400，不截断 | ✅（`TestExposure_AggregatesFromRiskEvents` / `TestExposure_RefusesTruncatedAggregate`） |
| 告警事件投影非法 | 非法告警被静默吞掉 | 计入 `invalid_events`，不进入列表；严重度词表外同样计非法 | ✅（`TestAlerts_ProjectsAndSortsFromRiskEvents`） |
| 汇总建立在截断数据上 | 少算告警 | `evaluate` 触到 limit → 400；`alerts` 分页截断显式 `truncated` | ✅（`TestEvaluate_ReportsWithoutWritingOrDetecting`） |

## 4. 不变量（可被测试钉住的）

1. 任一 run 的 `risk_rules_hash` 能反查到唯一快照。✅ `TestResolveLatest_OnlyActiveRevisionsEnterSnapshot`（GetSnapshot 反查 + 同内容同 hash 幂等）
2. 同一 hash 的规则包字节一致。✅ `internal/httpapi` 的 `TestAPI_ResolveLatestAndSnapshotByteIdentity`（wire 级两次拉取逐字节相等）
3. 热路径零 HTTP。✅ `rule.ParseBundle` 是纯函数（bytes → Bundle，零 I/O 零网络）；wire 三端点只服务 run 前 1 次的取包
4. 未审批规则不可能生效（状态机测试）。✅ `internal/rule` 的 `TestApprove_StateMachineAndSupersession`；draft 永不进快照（`TestResolveLatest_OnlyActiveRevisionsEnterSnapshot`）
5. `version_hash` 只由语义字段派生：params 空白 / 键序不同同 hash；`description` / `status` / 时间不参与。✅
6. 每次写操作都有审计行，且审计顺序与状态迁移顺序一致（create / update / approve / deprecate）。✅
7. 定义级 `CHECK ((status='active') = (active_revision IS NOT NULL))` 恒成立——新 draft 修订不清空 `active_revision`。✅
8. `risk_rules_hash` 只由 active 快照内容派生（排序 + canonical params，不含 `created_at`）；钉住的历史 hash 永不失效、不回退 latest。✅ `TestSnapshotHash_IsContentAddressed` / `TestResolve_PinnedHashNeverFallsBack`
9. 写路径的「读-改-写」全部在 `Store.Tx` 内；中途失败整笔回滚（Memory 与 PG 同语义）。✅ `TestMemoryTx_RollsBackAllWrites` / `TestServiceApprove_RollsBackAllWritesOnFailure`（ADR-0003）
10. 事件按 `(run_id, order_id, event_seq)` 幂等：同键重放只计 duplicate、不重复落库；未知字段保留在原文。✅ `TestIngest_DedupesByRunOrderSeq` / `TestIngest_PartialFailureKeepsValidItems`
11. 熔断指令按 `breaker_id` 幂等：同键同载荷重放 = `already_applied`（ts 不参与载荷身份）；同键不同载荷 = 409，不重复执行。✅ `TestApply_IdempotencyKey` / `TestAPI_BreakerIdempotency`
12. 敞口 / 告警 / 评估**只来自 `risk.risk_events`**：响应 `source` 固定，测试用真实 `POST /events` 回传后读取。✅ `TestMonitor_ExposureFromEvents` / `TestMonitor_AlertsAndEvaluate`
13. `evaluate` 不运行检测器、不写库：`detection:"outside_process"`，调用前后事件行数不变。✅ `TestEvaluate_ReportsWithoutWritingOrDetecting` / `TestMonitor_AlertsAndEvaluate`
14. 所有端点未装配鉴权闸时拒绝服务（503），角色不足 403；审计 actor 非空。✅ `TestMonitor_NoAuthorizerFailsClosed` / `TestAuth_ThreeStatesAndActor` / `TestRouteTableEqualsPolicyTable`
15. 路由表 ≡ 策略表（每个注册路由都有且仅有一个策略条目）。✅ `TestRouteTableEqualsPolicyTable`
16. `service` 角色只声明在 `POST /api/risk/resolve` 与 `POST /api/risk/events` 两条端点上，且与 `trader` / `admin` **同在一条 `Roles`** 里（any-of 且 kit 不排序 ⇒ 不能互相顶替）；`GET /api/risk/audit` 与规则管理面用 `service` 一律 403；新增档位不放松既有语义（`viewer` 调 `resolve` 仍 403）。✅ `TestPolicy_ServiceRoleScope` / `TestPolicy_ServiceRoleDeclaredExactlyTwice` / `TestPolicy_ServiceRoleCoexistsWithHumanRoles`（ADR-0006）
17. `params` 的键集合与单位由参数字典唯一裁定：写侧未知键 / 缺必填 / 越域即拒（不落库、不推进 head）；读侧只拒未知键、**不**重校取值域（历史快照仍可读，不变量 8 不被牵连）。✅ `TestParamsDictionary_IsPinned` / `TestValidate_RejectsParamsOutsideDictionary` / `TestParseBundle_RejectsUnknownParamKey` / `TestParseBundle_DoesNotRecheckValueDomain`
18. 监控空集可判别：响应必带 `configured`；未接入（false）与已接入但没触发（true + 空）取值不同；读失败不得落成空集。✅ `TestMonitor_EmptySetIsDiscriminable` / `TestMonitor_PGUnreachableIsNotEmptySet`（G4）

## 5. 与契约文档的关系

- 表结构 → `docs/db-schema.md`
- 接口形状 → `docs/api-schema.md`
- 版本与审批语义 → `docs/adr/0002-规则版本-hash-与审批状态机的裁决.md`
- 本文件只讲**流向与顺序**，不重复字段定义。

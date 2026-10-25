---
status: active
last-verified: 2026-10-24
verified-by: QT-42 监控 3 端点——risk_events 单源聚合、evaluate 只读报表与 fail-closed 鉴权闸
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
core 进程内 RiskPolicy  ◄── 每笔 CheckOrder（不走网络）   🔶 边界已备（rule.ParseBundle），
   │                                                        core 侧消费接线未做（run 的 risk_rules_hash 仍写空串，F3）
   └── 异步事件回传 ──►  POST /api/risk/events ──►  risk.risk_events   ✅ X3c（批量 + 去重键 + 207）
                                                          │
                                                          └──► monitor/{exposure,alerts,evaluate}
                                                               只读聚合 / 报表   ✅（2026-10-24，ADR-0004）
```

**现状（2026-10-24）**：左上半段（规则真相源 ①~③）、快照读侧（④ resolve / snapshots / bundle）、
事件回传（run 后批量）与监控三端点（只读聚合 / 报表）已实现，持久化有 Memory 与 PG（事务边界见 ADR-0003）；
core 侧消费接线、熔断执行方、告警生产侧接线（core `alertLoop` → `POST /events`）未落地，
未落项逐条登记在 `docs/TASKS.md` QT-42 施工记录。

## 2. 关键时序

### 规则变更（已实现）

1. `POST /api/risk/rules`：校验冻结枚举与 params（JSON 对象、≤64 KiB、拒尾随内容）→ 规范化 →
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

### run 前（粗粒度，一次）—— ✅ X3b（快照侧）；core 侧接线未做

5. 调用方 `POST /api/risk/resolve`（`{"hash": ...}` **或** `{"ruleset": "latest"}`，二选一；
   都给 / 都不给 / ruleset 非 latest → 400 `invalid_params`）
6. latest：服务把**当前 active revisions**收进快照，`risk_rules_hash = H(域, 排序后的 active 内容)`
   （不含 `created_at`，同一内容永远同一 hash）→ 按 hash 幂等落 `rule_snapshots`；
   没有任何生效规则 → 404 `no_approved_rules`（拒绝启动，不许用默认限额）
7. 钉 hash：按 hash 取历史快照；不存在 → 404 `snapshot_not_found`，**不回退 latest**
   （拿不到就拒绝开跑，不许用「现在生效的」顶替「当初锁定的」）
8. `GET /api/risk/snapshots/{hash}/bundle` 拉规则包（= canonical snapshot JSON）→
   进程内 `rule.ParseBundle`：重新 derive hash 比对（不符 fail-closed）+ 未知 type/scope 拒收
   → core 侧据此构建 `RiskPolicy`（**接线未做**，见主链路现状）
9. 该 hash 写入本次 run 的结果（可复现锚点）——core 侧接线未做（F3，QT-63 生产者仍写空串）

### run 中（热路径，零网络）—— 边界已备，core 接线未做

10. 每笔 `CheckOrder(order)` → 进程内裁决 → 通过 / 拒绝 / 降规模
11. 事件进本地队列

### 鉴权闸（所有端点，同步；2026-10-25）

0. 每个请求先过 `requireAuth`：未装配 `Authorizer` → `503 auth_unavailable`；有 secret 时按 kit
   `auth/httpadapter` 验 `Bearer` access token（拒 refresh / 坏签名 / 过期）→ 401，角色不在策略表 → 403；
   通过后把 `Principal` 放进 context，审计 `actor` 从这里取（open-access 记 `open-access`）。
   策略表（读 viewer / 写 trader / 审批 admin）与路由表由守卫测试双向对拍。

### run 后（异步）—— ✅ X3c / X3d / 监控（只读）

12. 批量 `POST /api/risk/events` → 逐条校验（缺 `run_id` / `order_id` / `ts` 记 failed）→
    合法条目按 `(run_id, order_id, event_seq)` 幂等落 `risk_events`：已存在 = duplicate（200 计数），
    单条不合法 = 207 `partial_failure` + `failed[{index, code, message}]`；整批落库失败 = 500，重试整批
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

## 3. 失败模式（每一处 fail-closed 都要能指名）

| 失败点 | 症状 | 兜底 | 现状 |
|---|---|---|---|
| 规则参数不是 JSON 对象 / 含尾随内容 / 超 64 KiB | 规则语义不可寻址 | 400 `invalid_params`，**不落库、不推进 head** | ✅ |
| `version_hash` 与内容不一致（Store 层） | 内容寻址被破坏 | `ErrRevisionConflict`，拒绝写入；`UNIQUE (rule_id, version_hash)` 兜底 | ✅ |
| 编辑基准过期 | 覆盖别人的修改 | 409 `revision_conflict`，要求重读 head | ✅ |
| 未审批规则被当成生效版本 | 生产用上草稿 | 只有 `active_revision` 可达 runtime；`draft` 永不进快照 | ✅（状态机 + DDL CHECK） |
| 并发写同一 rule_id | 丢更新 / 双 active | Service 的读-改-写全部在 `Store.Tx` 内：PG 用 `SELECT ... FOR UPDATE` 行锁，Memory 用全量快照回滚；失败整笔回滚 | ✅（ADR-0003；`TestServiceApprove_RollsBackAllWritesOnFailure` / `TestMemoryTx_RollsBackAllWrites`） |
| 快照 hash 与内容不符（写入 / 读取 / ParseBundle） | 内容寻址被破坏（缓存串号 / 内容被改） | `ErrSnapshotMismatch`，500 `internal` + 告警日志，fail-loud 不吞 | ✅（三处同检：`store.go` 写/读 + `ParseBundle`） |
| 服务不可用 | 拿不到规则快照 | **拒绝开跑**（不许用默认限额） | ✅（resolve 侧：`no_approved_rules` / `snapshot_not_found` 拒绝；core 接线后由调用方执行） |
| 规则被改但 run 里没记 hash | 结果不可复现 | `risk_rules_hash` 必写进结果 | ⛔ core 侧接线（F3） |
| 事件回传失败 | 留痕缺失 | 批量原子落库：整批失败 500 后重试整批（去重键幂等）；单条不合法 207 只重发 failed；允许延迟，不允许丢 | ✅（X3c：`internal/event` + `risk.risk_events` 主键去重） |
| 熔断指令重复 | 重复平仓 | 幂等键 `breaker_id`：同键同载荷 → 200 `already_applied`；同键不同载荷 → 409（不执行） | ✅（X3d：`internal/breaker` + `circuit_orders` 主键） |
| 鉴权闸未装配 / 角色不足 | 规则与审计被匿名读写 | 所有端点统一 fail-closed：未装配 `Authorizer` → 503，无凭证 → 401，越权 → 403 | ✅（`httpapi.requireAuth`；`TestAuth_ThreeStatesAndActor` / `TestRouteTableEqualsPolicyTable`） |
| 监控读不到事件（读取端口未装配 / 库错误） | 把「读不到」显示成「没有敞口 / 没有告警」 | `503 dependency_unavailable` 或 500；绝不返回空报告冒充成功 | ✅（`event.ErrMonitorUnavailable`；`TestMonitor_RequiresReader`） |
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

## 5. 与契约文档的关系

- 表结构 → `docs/db-schema.md`
- 接口形状 → `docs/api-schema.md`
- 版本与审批语义 → `docs/adr/0002-规则版本-hash-与审批状态机的裁决.md`
- 本文件只讲**流向与顺序**，不重复字段定义。

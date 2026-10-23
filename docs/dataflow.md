---
status: active
last-verified: 2026-10-23
verified-by: QT-42 规则侧实装——create/update/approve/audit 时序与 internal/rule 逐段对表
---

# Dataflow — tellurion-risk

> 目的：把「数据从哪来、在谁手里变形、到哪去」写死。**任何跨仓数据交换都必须能在本图上找到一条边**；找不到 = 这段代码在偷偷建第二条路。

## 1. 主链路

```
人 / 审批
   │  ① create → ② update（新 revision）→ ③ approve（activate）
   ▼
tellurion-risk  ── 写 risk.rule_defs / risk.rule_revisions / risk.audit_logs ──►  （schema 唯一 owner）
   │
   │  ④ resolve / bundle（run 前 1 次）            ⛔ X3b 未实现
   ▼
core 进程内 RiskPolicy  ◄── 每笔 CheckOrder（不走网络）   ⛔ X3b 之后才能构建
   │
   └── 异步事件回传 ──►  POST /api/risk/events ──►  risk.risk_events   ⛔ X3c
```

**现状（2026-10-23）**：只有左上半段（规则真相源 ①~③）实现；`resolve/bundle`、事件回传、熔断、监控均未落地，
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

### run 前（粗粒度，一次）—— ⛔ X3b

5. 调用方 `POST /api/risk/resolve`（`{rules: "latest"}` 或 `{hash}`）
6. 服务返回快照元数据（`risk_rules_hash`）
7. `GET /api/risk/snapshots/{hash}/bundle` 拉规则包 → 进程内构建 `RiskPolicy`
8. 该 hash 写入本次 run 的结果（可复现锚点）

### run 中（热路径，零网络）—— ⛔ X3b

9. 每笔 `CheckOrder(order)` → 进程内裁决 → 通过 / 拒绝 / 降规模
10. 事件进本地队列

### run 后（异步）—— ⛔ X3c / X3d

11. 批量 `POST /api/risk/events` → 落 `risk_events`
12. 监控层聚合敞口与告警；越限触发 `POST /breakers`（U7 未裁 ⇒ 监控三端点形状不冻结）

## 3. 失败模式（每一处 fail-closed 都要能指名）

| 失败点 | 症状 | 兜底 | 现状 |
|---|---|---|---|
| 规则参数不是 JSON 对象 / 含尾随内容 / 超 64 KiB | 规则语义不可寻址 | 400 `invalid_params`，**不落库、不推进 head** | ✅ |
| `version_hash` 与内容不一致（Store 层） | 内容寻址被破坏 | `ErrRevisionConflict`，拒绝写入；`UNIQUE (rule_id, version_hash)` 兜底 | ✅ |
| 编辑基准过期 | 覆盖别人的修改 | 409 `revision_conflict`，要求重读 head | ✅ |
| 未审批规则被当成生效版本 | 生产用上草稿 | 只有 `active_revision` 可达 runtime；`draft` 永不进快照 | ✅（状态机 + DDL CHECK） |
| 并发写同一 rule_id | 丢更新 / 双 active | 当前进程内互斥锁串行；**接 PG 后必须换事务**（QT-42 未落项） | 🔶 |
| 服务不可用 | 拿不到规则快照 | **拒绝开跑**（不许用默认限额） | ⛔ X3b |
| 规则被改但 run 里没记 hash | 结果不可复现 | `risk_rules_hash` 必写进结果 | ⛔ X3b |
| 事件回传失败 | 留痕缺失 | 本地队列 + 重试；允许延迟，不允许丢 | ⛔ X3c |
| 熔断指令重复 | 重复平仓 | 幂等键 | ⛔ X3d |

## 4. 不变量（可被测试钉住的）

1. 任一 run 的 `risk_rules_hash` 能反查到唯一快照。（⛔ X3b）
2. 同一 hash 的规则包字节一致。（⛔ X3b）
3. 热路径零 HTTP（可用调用栈测试钉住）。（⛔ X3b）
4. 未审批规则不可能生效（状态机测试）。✅ `internal/rule` 的 `TestApprove_StateMachineAndSupersession`
5. `version_hash` 只由语义字段派生：params 空白 / 键序不同同 hash；`description` / `status` / 时间不参与。✅
6. 每次写操作都有审计行，且审计顺序与状态迁移顺序一致（create / update / approve / deprecate）。✅
7. 定义级 `CHECK ((status='active') = (active_revision IS NOT NULL))` 恒成立——新 draft 修订不清空 `active_revision`。✅

## 5. 与契约文档的关系

- 表结构 → `docs/db-schema.md`
- 接口形状 → `docs/api-schema.md`
- 版本与审批语义 → `docs/adr/0002-规则版本-hash-与审批状态机的裁决.md`
- 本文件只讲**流向与顺序**，不重复字段定义。

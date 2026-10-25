---
status: active
last-verified: 2026-10-25
verified-by: 裁-15 鉴权接线（kit net/http 适配 + 全端点策略表 + 审计读 + actor 取自 claims）；裁-17 服务间角色 `service`（`resolve` / `events` 两条，ADR-0006）；QT-42 X3c 的 core 腿（告警生产侧接线已落，`alerts` / `evaluate` 不再是结构性空集）
---

# API Schema — tellurion-risk

> **实现状态（2026-10-25）**：**规则管理 4 端点 + 快照 3 端点 + 审计读 1 端点 + 事件回传 1 端点 +
> 熔断 1 端点 + 监控 3 端点已落地，且全部过鉴权闸**
> （`POST /rules`、`PUT /rules/{id}`、`POST /rules/{id}/approve`、`GET /rules`、`POST /resolve`、
> `GET /snapshots/{hash}`、`GET /snapshots/{hash}/bundle`、`GET /audit`、`POST /events`、`POST /breakers`、
> `GET /monitor/exposure`、`GET /monitor/alerts`、`POST /monitor/evaluate`）。
> 实现在 `internal/rule`（快照内容寻址 + `ParseBundle` 进程内边界）、`internal/event`（事件校验 + 去重键 +
> **监控聚合读**）、`internal/breaker`（熔断指令 + 幂等键），传输在 `internal/httpapi`
> （标准库 `net/http`，不是 gin），持久化有 Memory 与 PG（`internal/pgstore`，启动时执行幂等 DDL；
> 事务边界见 ADR-0003）。
> **鉴权（2026-10-25，裁-15 + ADR-0005）**：机制在 kit `auth/httpadapter`（Bearer → `Parse` →
> `kind=access` → 角色 any-of），策略表在本仓 `internal/httpapi/policy.go`；**所有端点**都过闸，
> 未装配 `Authorizer` → **503 `auth_unavailable`**（拒绝而非放行）。读端点 viewer、写端点 trader、
> 规则发布 / 审批 admin。无 secret + 回环 = 开放；无 secret + 非回环 = **拒绝启动**。
> 审计 `actor` 从 claims 填（open-access 记 `open-access`），不再有空串。
> **服务间档 `service`（2026-10-25，裁-17 + ADR-0006）**：`service` 是**服务间**角色（core 代机器调用），
> 与人角色正交、不是「比 admin 低一档」；只开 `POST /api/risk/resolve` 与 `POST /api/risk/events`
> 两条端点，`GET /api/risk/audit` 与规则管理面**不开**。角色 any-of 且 kit 不做排序 ⇒ `service`
> 与 `trader` / `admin` **并列**写在同一条 `Roles` 里。
> **监控三端点的检测器不搬出 core**：它们只读 `risk.risk_events` 做聚合与报表，`evaluate` **不运行检测器**
> （ADR-0004）。
> **未落地**：外部推送通道；`POST /events` **不写审计行**（`actorOf` 今天只作用于规则 create/update/approve
> 三处，`risk.risk_events` 也没有 actor / account 列 —— 事件回传的调用方身份不落库，见 `docs/TASKS.md` 未落项）。
> **已落（2026-10-25 订正，原先这两条记在「未落地」里）**：
> ① **告警生产侧接线**：core 的 `core:pkg/alert`（6 个检测器 + 经 `AlertManager.Fire` 进来的对账 / 双跑差异）
> 已按 `POST /api/risk/events` 回传 `verdict:"alert"` 的事件，落点在 `core:cmd/analysis/wiring/risk_events.go`
> （装配开关 `risk_service.events_enabled`，**默认关**）。`monitor/alerts` / `evaluate` 因此不再是结构性空集：
> 开关打开的部署里有数据，关着的部署里空集是**设定**而非缺陷。
> ② **core 侧消费接线（F3）**：QT-66 已落 —— core 每次 run 提交时调 `POST /api/risk/resolve`，把
> `risk_rules_hash` 写进 `quant.run_provenance`；该列不再恒空串（空串只剩「本部署未接入」一种合法含义，
> 记 `not_configured`）。未落项逐条登记在 `docs/TASKS.md` QT-42 施工记录。
>
> 本文件是**目标契约 + 现状**：已实现的行以代码为准，未实现的行是欠账，不允许把欠账当现状描述。

> **契约地位**：HTTP，但**只有粗粒度操作走 HTTP**（run 前解析、事件回传、熔断、监控）。热路径的 `CheckOrder` 是进程内 port。
> 本文件是**本仓对外接口的唯一真相源**。代码与它不一致时，先判断谁错：契约错 → 改本文 + 立 ADR；实现错 → 改代码。
> 跨仓消费方（core / agent / ui / 其他服务）按本文件编码，**不读本仓实现**。
> **请求 / 响应形状**：本文只冻结**路径、错误码、幂等与兼容**；逐端点的目标签名、DTO 字段与版本策略见 `ARCHITECTURE.md` §9（M8 接口对表），两处必须同步改，不复制形状。

## 1. 接口面

### 规则管理（写侧，人 / 审批）—— ✅ 已实现（2026-10-23）

| 方法 | 路径 | 用途 | 现状 |
|---|---|---|---|
| POST | `/api/risk/rules` | 建规则（draft）→ 201；同内容重放 → 200 | ✅ |
| PUT | `/api/risk/rules/{id}` | 改规则 → 生成新 revision（draft） | ✅ |
| POST | `/api/risk/rules/{id}/approve` | 审批激活（draft → approved → active） | ✅ |
| GET | `/api/risk/rules?scope=&active=true` | 查询（列表项 = head revision 摘要） | ✅ |

### 审计读（viewer）—— ✅ 已实现（2026-10-25）

| 方法 | 路径 | 用途 | 现状 |
|---|---|---|---|
| GET | `/api/risk/audit?rule_id={id}` | 查某条规则的写操作审计（create / update / approve / deprecate） | ✅ |

> 响应 `{"audit":[{"seq","rule_id","revision","action","actor","detail?","at"}],"count":N}`；
> `rule_id` 必填（缺失 → `400 invalid_params`），空结果编码为 `[]`。
> `actor` 来自鉴权 claims（`username`，缺失退 `subject`）；显式 open-access 记 `open-access`，**永不空串**。
> 审计是**人操作留痕**，与 core 的鉴权访问审计（ADR-017）不是同一张表。

### 运行接入（读侧，内核 / 实盘）—— 快照三端点 ✅ 已实现（2026-10-23，X3b）

| 方法 | 路径 | 用途 | 现状 |
|---|---|---|---|
| POST | `/api/risk/resolve` | 规则集引用（锁定 hash 或 latest，二选一）→ 快照元数据 `{risk_rules_hash, snapshot}` | ✅ |
| GET | `/api/risk/snapshots/{hash}` | 取规则快照（同一 hash 两次拉取**字节一致**；按 hash 缓存永不失效） | ✅ |
| GET | `/api/risk/snapshots/{hash}/bundle` | 拉规则包（run 前 1 次；body = canonical snapshot JSON，`rule.ParseBundle` 直接吃） | ✅ |
| POST | `/api/risk/events` | 回传裁决事件（异步批量，**非热路径**） | ✅ X3c（2026-10-24） |
| POST | `/api/risk/breakers` | 下发熔断指令（幂等键） | ✅ X3d（2026-10-24） |

> **服务间调用（2026-10-25，裁-17 + ADR-0006）**：`POST /api/risk/resolve` 与 `POST /api/risk/events`
> 这两条是 **core 代机器发起**的链路，策略表对它们额外开 `service` 角色（与 `trader` / `admin` 并列）。
> 其余端点（含 `GET /api/risk/audit`、`POST /api/risk/breakers`、`monitor/*`、规则管理面）**不开** `service`：
> 审计与审批是人负责的动作，机器凭证不代劳。新增档位**不放松**既有语义——`viewer` 调 `resolve` 仍 403。
> 守卫：`TestPolicy_ServiceRoleScope` / `TestPolicy_ServiceRoleDeclaredExactlyTwice` /
> `TestPolicy_ServiceRoleCoexistsWithHumanRoles`（`internal/httpapi/policy_test.go`）。

### 监控（聚合层）—— ✅ 已实现（2026-10-24，只读 `risk.risk_events`）

| 方法 | 路径 | 用途 | 现状 |
|---|---|---|---|
| GET | `/api/risk/monitor/exposure` | 跨策略 / 账户 / 标的敞口（按事件 payload 聚合） | ✅，需鉴权闸 |
| GET | `/api/risk/monitor/alerts` | 告警列表（`verdict=alert` 的事件投影） | ✅，需鉴权闸 |
| POST | `/api/risk/monitor/evaluate` | 按口径对已回传告警做一次报表评估（**不运行检测器**） | ✅，需鉴权闸 |

> `GET /api/risk/rules` 的列表项：`{rule_id, revision, status, version_hash, scope, type, description?, active_revision?}`；
> `status` 是 **head revision** 的状态（新建 draft 修订后 head 是 `draft`，即使上一条仍 `active`）；
> `active_revision` = 当前生效的 revision（缺省 = 从未激活）。空列表编码为 `{"rules":[],"count":0}`，**不是 `null`**。
>
> `POST /api/risk/events`（X3c）：
> 请求 `{"events":[{run_id, order_id, event_seq, symbol?, verdict?, reason?, payload?, ts}]}`；
> 全部成功 → `200 {accepted, duplicates}`；部分条目失败 → `207 {accepted, duplicates, failed:[{index, code, message}], code:"partial_failure"}`。
> 去重键 `run_id + order_id + event_seq`；未知字段不丢（落库原文进 `raw` JSONB）。
> 整批不可解析（含空数组）→ `400 invalid_params`；落库失败 → `500 internal`，调用方重试整批（去重键保证幂等）。
>
> `POST /api/risk/breakers`（X3d）：
> 请求 `{breaker_id, action, scope, reason?, ts}` → `200 {breaker_id, status:"applied"|"already_applied"}`。
> 冻结动作枚举 `halt`（停机）/ `cancel_all`（撤单）；`scope` 复用规则的冻结枚举。
> 幂等键 `breaker_id`；**载荷身份 = action + scope + reason**（`ts` 不参与：重试可重新生成时间）；
> 同键不同载荷 → `409 idempotency_conflict`；未知 action / scope / 缺字段 → `400 invalid_params`。

### 监控三端点（2026-10-24）

> **数据源只有 `risk.risk_events`**（`source` 字段固定回显 `risk.risk_events`）。本仓不读订单 / 持仓，
> 也不运行检测器；告警由 core 的 detector 产出后按 `POST /events` 回传（`verdict:"alert"`）。
> **时间窗**：`from` / `to` 为 RFC3339，按事件的 `ts` 闭区间过滤；`to` 缺省 = 当前时间，
> `from` 缺省 = `to - 24h`；`from >= to` 或跨度 > 7 天 → `400 invalid_params`。
> **鉴权**：三端点都必须过鉴权闸；闸未装配 → `503 auth_unavailable`，无凭证 → `401 unauthenticated`，
> 角色不足 → `403 forbidden`。**拒绝，不是放行**。

**`GET /api/risk/monitor/exposure`**

查询参数：`from` / `to`、`group_by=symbol|account|strategy`（缺省 `symbol`）、`run_id`、`symbol`、`verdict`。

```json
{
  "window": {"from": "2026-10-23T12:00:00Z", "to": "2026-10-24T12:00:00Z"},
  "group_by": "symbol",
  "source": "risk.risk_events",
  "events": 3,
  "unpriced_events": 1,
  "unsigned_events": 1,
  "totals": {"gross_notional": 1900, "net_notional": 600, "long_notional": 1000, "short_notional": 400},
  "groups": [
    {"key": "600000.SH", "events": 2, "unpriced_events": 0, "unsigned_events": 0,
     "gross_notional": 1400, "net_notional": 600, "long_notional": 1000, "short_notional": 400}
  ]
}
```

- 名义金额：`|payload.notional|`，否则 `|payload.qty| × |payload.price|`；两者都没有 → 计入 `unpriced_events`，
  **不补零**（缺数据必须显性）。
- 方向：`payload.side` 为 `buy` / `sell`；缺失时用 `qty` 的符号推断；仍推不出 → 计入 `unsigned_events`
  （只进 `gross_notional`，不进 `net_notional`）。
- `verdict=alert` 的事件**不参与**敞口（发现不是成交，避免与裁决事件重复计数）。
- 分组键缺失 → `unattributed` 桶（不静默丢弃）；`groups` 按 key 升序，空结果编码为 `[]`。
- 窗口内事件数 > 5000 → `400 invalid_params`（**拒绝而不是截断**：截断的敞口会低报风险）。

**`GET /api/risk/monitor/alerts`**

查询参数：`from` / `to`、`severity=info|warning|critical`、`rule`、`run_id`、`symbol`、
`limit`（缺省 100，上限 1000）。

```json
{
  "window": {"from": "2026-10-23T12:00:00Z", "to": "2026-10-24T12:00:00Z"},
  "source": "risk.risk_events",
  "alerts": [
    {"id": "run-1:o-b:1", "rule": "position_concentration", "severity": "critical",
     "message": "position 40%", "symbol": "000001.SZ", "run_id": "run-1",
     "order_id": "o-b", "event_seq": 1, "ts": "2026-10-24T11:05:00Z"}
  ],
  "count": 1,
  "truncated": false,
  "invalid_events": 1
}
```

- 只读 `verdict="alert"` 的事件；`payload` 投影：`id?` / `rule` / `severity` / `message` 必填，
  `value?` / `threshold?` / `attributes?` 可选；`id` 缺省 = `run_id:order_id:event_seq`。
- `rule` / `severity` / `message` 缺失或 `severity` 不在冻结词表 → 计入 `invalid_events`，**不静默丢弃**。
- 按事件时间倒序；`limit` 作用在**合法告警**上，原始告警行扫描上限 1000（触顶 → `truncated:true`）。
- 空结果编码为 `[]`。

**`POST /api/risk/monitor/evaluate`**

请求（严格 JSON，未知字段拒收；`{}` 合法）：

```json
{"from": "2026-10-23T12:00:00Z", "to": "2026-10-24T12:00:00Z",
 "run_id": "run-1", "rule": "drawdown", "symbol": "600000.SH",
 "min_severity": "warning", "limit": 1000}
```

响应：

```json
{
  "evaluated_at": "2026-10-24T12:00:00Z",
  "window": {"from": "2026-10-23T12:00:00Z", "to": "2026-10-24T12:00:00Z"},
  "source": "risk.risk_events",
  "detection": "outside_process",
  "criteria": {"min_severity": "warning"},
  "alerts": [
    {"id": "run-1:o-a:1", "rule": "drawdown", "severity": "warning",
     "message": "drawdown 12%", "symbol": "600000.SH", "run_id": "run-1",
     "order_id": "o-a", "event_seq": 1, "ts": "2026-10-24T11:00:00Z",
     "value": 0.12, "threshold": 0.1}
  ],
  "summary": {"total": 1, "by_severity": {"warning": 1}, "by_rule": {"drawdown": 1}},
  "invalid_events": 1
}
```

- **不运行任何检测器**（`detection:"outside_process"`），**不写库**，可安全重试。
- `min_severity` 缺省 `info`；非法值 → `400 invalid_params`。
- `limit` 缺省 / 上限 1000；结果被截断（合法告警数超过 `limit`）→ `400 invalid_params`，
  **不允许汇总建立在截断数据上**。

**事件 `payload` 的监控投影（生产侧约定）**

| 字段 | 被谁用 | 含义 |
|---|---|---|
| `account` / `strategy` | exposure | 分组键（`group_by=account` 或 `strategy`） |
| `side` | exposure | `buy` / `sell`；缺省时由 `qty` 符号推断 |
| `qty` / `price` / `notional` | exposure | 名义金额；`notional` 优先，`qty×price` 次之 |
| `id` / `rule` / `severity` / `message` | alerts / evaluate | 告警身份与展示（`severity` 冻结 `info` / `warning` / `critical`） |
| `value` / `threshold` / `attributes` | alerts / evaluate | 可选证据（attributes 原样透传） |

> 冻结口径：这些字段是**新增**（MINOR）；改名 / 改语义 = breaking。未知字段不影响解析（payload 仍是自由审计载荷）。
> **生产侧已接线（2026-10-25，QT-42 X3c 的 core 腿）**：core 的告警按上表投 `POST /events`，去重键的映射是
> `run_id = "core:alert-loop"`（生产者作用域，**不是**一次真实 run）、`order_id = Alert.ID`、`event_seq = 0`。
> 三条后果如实登记：`?run_id=` 过滤对告警只有生产者含义；告警不带 `notional` / `qty`，在 `exposure` 里一律计入
> `unpriced_events`（**不改变敞口口径**）；`payload.attributes` 里若有不可编码值（NaN / Inf）core 侧当场报错并
> **不发该条**，所以缺告警可能是生产侧投递失败，不一定是「没触发」。

## 2. 错误码与 fail-closed 语义

> 错误码与 `ARCHITECTURE.md` §9.4 **双向一一对应**；本节是 code 的**唯一词表**，新增 code 必须先改这里。

| 情况 | 返回 | 调用方行为 |
|---|---|---|
| 无凭证 / token 过期 | 401 `unauthenticated` — **✅ 全部端点**（2026-10-25） | 不重试，补齐凭证 |
| 角色不足（读需 viewer / 写需 trader / 审批需 admin；`resolve` / `events` 另开 service） | 403 `forbidden` — **✅ 全部端点**（2026-10-25） | 不重试 |
| 鉴权闸未装配 | 503 `auth_unavailable` — ✅ 全部端点（**拒绝而非放行**） | 不重试；等待运维装配 Authorizer |
| 监控读取端口未装配 | 503 `dependency_unavailable` — ✅ | 不重试；服务端装配问题 |
| 监控时间窗非法（`from >= to` / 超 7 天）、事件数或告警数超上限 | 400 `invalid_params` — ✅ | 缩小窗口 / 降低 limit 后重试 |
| 规则类型不在冻结枚举 | 400 `invalid_rule_type` | 修正请求，不重试 |
| 参数非法（非 JSON 对象 / 空 / 超 64 KiB）、scope 非法、请求体非法 | 400 `invalid_params` | 修正请求，不重试 |
| 同一幂等键（`rule_id` / `breaker_id`）不同内容 | 409 `idempotency_conflict` | **当冲突处理**，不许当成功 |
| 规则不存在 | 404 `rule_not_found` | 不重试 |
| 编辑基准 `base_revision` 过期 | 409 `revision_conflict` | 重读 head 后再改 |
| 编辑已废弃规则 | 409 `rule_deprecated` | ⛔ **本切片不产生**（见下注） |
| 审批的 revision 不是 draft | 409 `revision_not_draft` | 不重试 |
| revision 不存在 | 404 `revision_not_found` | 不重试 |
| 无 approved 版本 | 404 `no_approved_rules` — ✅ | run 前拒绝启动 |
| 规则 hash 不存在 | 404 `snapshot_not_found` — ✅ | 拒绝（不回退 latest） |
| 熔断指令重复（同键同载荷） | 200 + `already_applied` — ✅ X3d | 视为成功（幂等） |
| 事件批量部分失败 | 207 + `partial_failure` + 失败明细 — ✅ X3c | 重试失败项，不重发全量 |
| 其它 | 500 `internal`（**不吐内部原文**） | 服务端问题 |

> **`rule_deprecated` 为什么本切片不产生**：冻结的接口面里没有 deprecate 端点，「已废弃」只在 revision 级出现——
> 同规则新 revision 激活时旧 active 自动变 `deprecated`（撤回不是删除，历史快照仍可按 hash 复现）。
> 定义级从未进入 `deprecated`，所以 PUT 的这条 409 没有可达触发点；实现里**没有**写这条不可达分支。
> 若将来新增 deprecate 端点，必须先改本节再实现。
>
> **`scope` 非法用 `invalid_params` 而不是新造 `invalid_scope`**：§9.4 的错误码表只冻结了 `invalid_rule_type` 与
> `invalid_params` 两条输入错误；新造第四个码等于把词表劈成两份。
>
> **鉴权与 actor（2026-10-25）**：全部端点先过 kit `auth/httpadapter` 适配的鉴权闸；`actor` **不从请求体读取**
> （body 里带 `actor` 会被严格解码拒收），由鉴权上下文填 `username`（缺失退 `subject`）。
> 显式关闭鉴权（无 secret + 回环）时记常量 `open-access`；审计 actor **永不为空串**。
> 角色矩阵：读 viewer、写 trader、规则发布 / 审批 admin（`internal/httpapi/policy.go`）；
> `POST /api/risk/resolve` 与 `POST /api/risk/events` 另开 `service`（服务间角色，ADR-0006）。
> 策略表与路由表由 `TestRouteTableEqualsPolicyTable` 双向对拍；`service` 的作用域由
> `TestPolicy_ServiceRoleScope` / `TestPolicy_ServiceRoleDeclaredExactlyTwice` 双向对拍。

**fail-closed**：拿不到规则快照 = 不许开跑，不允许"先用默认限额"；没有凭证 / 角色不足 = 不服务，**不静默降级到匿名或 loopback 放行**（loopback-only insecure 仅限显式本地开发配置）。
监控三端点在本切片就把这条钉死：`Authorizer` 未装配时返回 `503 auth_unavailable`，**不注册任何"匿名可读"后门**；
`alerts` / `evaluate` 也不会把"读不到事件"或"解析失败的事件"当成"没有告警"——前者 503，后者计入 `invalid_events`。

## 3. 幂等性与重试

- `POST /rules` 的幂等键是 `rule_id`（调用方给）：同键同内容 → 200 + 现有 revision；同键不同内容 → 409 `idempotency_conflict`。
- `PUT /rules/{id}` 必须带 `base_revision`：不匹配当前 head → 409 `revision_conflict`（防丢更新）。
- `POST /rules/{id}/approve` 对**同一 revision** 重放幂等（同 `version_hash`）；状态机之外没有第二条路。
- `POST /breakers` 必须带幂等键（`breaker_id`），同键同载荷重复调用不重复执行（200 `already_applied`）；
  同键不同载荷 → 409 `idempotency_conflict`。载荷身份 = `action + scope + reason`，`ts` 不参与。✅
- `POST /events` 支持批量 + 去重键（`run_id + order_id + event_seq`）；部分失败返回 207，只重发失败项；整批落库失败返回 500，可整批重试（去重幂等）。✅
- `resolve` / `snapshots` / `rules`（GET）只读，可任意重试。
- `monitor/exposure` / `monitor/alerts`（GET）只读，可任意重试；同一窗口两次查询顺序与数值一致（排序固定）。
- `monitor/evaluate`（POST）**只读**：不写 `risk_events`、不写任何表、不推送外部通道；同一输入可安全重放。
  它不运行检测器，因此重放不会产生新的告警。

## 4. 版本与兼容

> 逐端点 / 逐符号的版本策略见 `ARCHITECTURE.md` §9.6；本节只留三条总则。

- 规则**类型**（枚举）：新增类型 = MINOR（旧读者必须 fail-closed 拒收未知类型）；改既有类型的语义 = breaking（老 run 的 `risk_rules_hash` 可比性失效）。
- 熔断**动作**（枚举）：新增动作 = MINOR（旧执行方必须 fail-closed 拒收未知动作）；幂等键语义 frozen。
- 端点只增不删；废弃走 `deprecated` 头 + 一个 MINOR 周期。
- 监控事件的 `payload` 投影字段（`account` / `strategy` / `side` / `qty` / `price` / `notional` /
  `id` / `rule` / `severity` / `message` / `value` / `threshold` / `attributes`）**加字段 = MINOR**；
  改名或改语义 = breaking。`severity` 词表与 core `pkg/alert.Severity` 同源（`info|warning|critical`）。
- 监控查询参数只增不删；`evaluate` 的 `detection:"outside_process"` 是**边界声明**，删掉它等于让调用方
  误以为检测发生在本仓，属 breaking。
- 快照格式变更必须保留旧格式读取器（否则历史回测不可复现）；hash 算法变更 = 域标签 v2，全历史 hash 作废。

## 5. 不提供什么（边界）

- 不做逐笔 / 逐 bar 的同步裁决（进程内 port）
- 不持有订单 / 持仓真相
- 不做券商通道对接（那是 broker）
- 不做回测（只提供"这次 run 该用哪套规则"）
- **不运行检测器**：compliance 六检测器 / 减持 / 大额报送、告警 6 条检测规则全部留在 core 进程内
  （U7 拆分，ADR-0004）；本仓的 `monitor/*` 只做聚合、报表与通道编排
- **不持有敞口真相**：敞口由 `risk_events.payload` 聚合而来，本仓不读订单 / 持仓；缺字段显性计为
  `unpriced_events`，不补零
- **不接管** core :8085 的四条进程内 risk 端点（`/api/risk/calculate_position`、`detect_regime`、`check_stoploss`、`metrics`）——它们是 in-process `RiskManager` 的调试面，热路径不经过 HTTP；本仓只拥有 `rules` / `snapshots` / `resolve` / `events` / `breakers` / `monitor`（§9.1 F4 / R1）。

---
status: active
last-verified: 2026-10-24
verified-by: QT-42 X3d 熔断指令实装（internal/rule · internal/event · internal/breaker · internal/pgstore · internal/httpapi · cmd/riskservice）
---

# 架构 — tellurion-risk

## 1. 定位

风控规则的**真相源** + 聚合监控与熔断。

关键判断：**同步裁决留在 core 进程内**。风控出仓的是"规则怎么定义、版本怎么管、跨策略敞口怎么看"，不是"每笔订单要不要放行"。

## 2. 分层

```
写侧（人 / 审批）                     读侧（内核 / 实盘）
  rule → revision → approve            resolve → bundle（run 前 1 次）
  → version_hash                       → RiskPolicy（进程内）
        │                                     │
        ↓                                     ↓
   risk.rule_defs / _revisions / _snapshots / limits / events / breakers / audit

   监控层：exposure / alerts / evaluate（聚合，异步）
```

## 3. 组件职责

| 组件 | 职责 | 不做 |
|---|---|---|
| 规则管理 | 定义 / 版本 / 审批状态机 | 不执行裁决 |
| 快照 | 内容寻址、永不失效 | 不解析规则语义 |
| 事件收集 | 异步批量落库 | 不在热路径 |
| 监控 | 敞口 / 告警 / 合规检测 | 不阻断下单（阻断靠熔断指令） |

## 3.1 包布局（现状，2026-10-23）

```
cmd/
  riskservice/       进程入口：PG（-dsn / RISK_DATABASE_URL，启动时 Migrate）或 Memory → rule.Service + httpapi
internal/
  rule/              领域核心：RuleType/Scope/Status 冻结枚举、Revision 校验与参数规范化、
                      VersionHash、快照内容寻址（Snapshot / SnapshotHash / ParseBundle）、
                      Store 端口（含 Tx 事务语义）+ Memory 实现、Migrations()（11 条幂等 DDL）、
                      Service（Create / Update / Approve / List / Audit / Resolve / GetSnapshot）
  pgstore/           PG 实现：rule.Store / event.Store / breaker.Store（Tx = 数据库事务 + FOR UPDATE 行锁）+ Migrate
  event/             X3c 事件：wire Event / 逐条校验 / 去重键 + Memory 实现 / Service.Ingest
  breaker/           X3d 熔断：wire Command / 冻结动作枚举 / 幂等键 + Memory 实现 / Service.Apply
  httpapi/           传输层：标准库 http.ServeMux 的 4 个规则端点 + 3 个快照端点 + 1 个事件端点 + 1 个熔断端点 + 严格解码 + wire code 映射
doc.go               仓级包文档（无代码）
```

**依赖方向单一**：`cmd/riskservice` → `internal/httpapi` / `internal/pgstore` → `internal/rule` / `internal/event` / `internal/breaker`；
`internal/rule` 不 import 传输层，也不 import `database/sql`（事务边界由 Store 端口表达，见 ADR-0003）。
**未落**：monitor 三端点（U7 未裁）、鉴权、
core 侧 RiskPolicy 消费接线（F3）。清单见 `docs/TASKS.md` QT-42 施工记录。

## 4. 三份契约（功能的边界就在这三份文件里）

| 维度 | 文件 | 回答什么问题 |
|---|---|---|
| 数据形状 | [`db-schema.md`](db-schema.md) | 本仓拥有哪些表？谁能写？ |
| 接口形状 | [`api-schema.md`](api-schema.md) | 对外暴露什么？错误码与幂等怎么做？ |
| 流向与顺序 | [`dataflow.md`](dataflow.md) | 数据怎么流？哪里 fail-closed？ |

## 5. 依赖方向

```
contracts（规则 DTO / 事件 DTO，S1 后落）+ kit（logging/http/db，未引入）
        ↑
 tellurion-risk ──(HTTP 粗粒度)──► core 进程内 RiskPolicy
        └──(HTTP 异步)──► 事件回传
```

- 规则侧**无跨仓依赖**：版本 hash 与枚举都在 `internal/rule` 自己定义（域标签风险见 `db-schema.md` §5）。
- 唯一第三方依赖是 PostgreSQL 驱动 `github.com/jackc/pgx/v5`（仅在进程入口注册驱动，见 ADR-0003）；
  领域包只依赖 `database/sql` 的抽象（`internal/pgstore`），不感知驱动实现。
- 契约（`RuleSnapshot` / `RuleEvent` / `BreakerCommand`）要等 X3b/X3c 出现第二个 Go 使用方后才按 ADR-0001 ① 迁 contracts。

## 6. 关键决策

- [`adr/0001-两层结构-真相源出仓裁决留内核.md`](adr/0001-两层结构-真相源出仓裁决留内核.md)
- [`adr/0002-规则版本-hash-与审批状态机的裁决.md`](adr/0002-规则版本-hash-与审批状态机的裁决.md)
- [`adr/0003-pg-store-与事务边界.md`](adr/0003-pg-store-与事务边界.md)

## 7. 非目标（明确不做）

- 不做同步裁决服务
- 不持有订单/持仓
- 不做券商对接
- 不做回测

## 8. 分期与状态

| 阶段 | 内容 | 状态 |
|---|---|---|
| X3a | 规则定义 + 版本 + 审批状态机 + 审计 | 🔶 已实装（`internal/rule` + `internal/httpapi` 4 端点；PG Store / 迁移执行 / 事务边界已落（2026-10-24，ADR-0003）；鉴权未落） |
| X3b | 快照 + `resolve` / `bundle` | 🔶 已实装（`risk.rule_snapshots` DDL + 快照内容寻址 + `resolve`/`snapshots/{hash}`/`bundle` 三端点 + `rule.ParseBundle` 进程内边界，Memory/PG 双实现）；core 侧消费接线（F3）未落 |
| X3c | 事件回传 + 监控聚合 | 🔶 事件回传已实装（`internal/event` + `POST /events`：批量、去重键、207 partial_failure）；监控聚合 ⛔ U7 未裁 |
| X3d | 熔断指令（幂等） | 🔶 已实装（`internal/breaker` + `POST /breakers` + `risk.circuit_orders`：`breaker_id` 幂等键、200 applied/already_applied、409 载荷冲突）；执行方与 ACK ⛔ 升格 ADR |

## 9. 接口对表（M8 前置 · ✅ 2026-10-22）

> **本节是什么**：`tellurion:docs/TASKS.md`「五、X 线」公共前置 ② 要求的**接口对表本体**（模板 = `tellurion:docs/design/qt-decoupling/data-service-contract-and-ownership.md` §4.1）。逐个 port / DTO / endpoint 列 **签名 + 错误语义 + 版本策略**；缺一列就是没对完表。
> **实测基线**：core `dbe4c68`（2026-10-22；QT-63 已落）。行号一律取 `git show HEAD:<path>`。复核方式：`git -C ../tellurion show HEAD:<path> | sed -n '<line>p'`。
> **列口径**：「今天」= 源码实测；「契约位」= S1（QT-05）后 contracts 的落点，写 `core-only` 表示今天**不满足** contracts ADR-0001 ① 的「≥2 个仓使用」判据（不是永久留 core）；「版本策略」按 QT-19 的 MAJOR / MINOR / PATCH 判定。
> **引用**：`tellurion:docs/design/qt-decoupling/kernel-interface-map.md` §5（风控注入点）与 §二（输出面）；`tellurion-contracts:docs/API.md:134`（`domain.RiskManager` 已登记）、`tellurion-contracts:docs/API.md:173`（`BacktestResult` 无 `status`）。

### 9.1 五条前置事实（每条都会改变施工判断）

| # | 事实 | 取证（core `dbe4c68`） | 对施工的后果 |
|---|---|---|---|
| F1 | **同步裁决 port 只有接口 + panic stub**：`*RiskManager` 实现的是 `domain.RiskManager` 三方法，**没有 `CheckOrder`**；注入面收的是**具体类型**。 | `pkg/risk/interfaces.go:65`（`RiskEngine` 四方法）、`:69`（`CheckOrder`）、`:93`（stub panic）、`:108`（`_ domain.RiskManager = (*RiskManager)(nil)`）；`pkg/domain/types.go:204`；`pkg/backtest/engine.go:1843`（`SetRiskManager(*risk.RiskManager)`）、`pkg/backtest/options.go:51`（`WithRiskManager(*risk.RiskManager)`）。`grep -rn 'CheckOrder(' --include='*.go' pkg cmd e2e \| grep -v _test` = 2 命中（声明 + stub），**零调用**。 | QT-42 ③-②「现状 `RiskManager` 已无状态，可直接适配」**只对三个冻结方法成立**；`CheckOrder` 是**净新增**（连同错误语义）。接口化必须改 core 的两个注入入口（内部 breaking，属实现单）。「无状态」的精确含义：字段全是构造期配置 + logger（`pkg/risk/manager.go:13/40`、`pkg/risk/stoploss.go:14-23`、`pkg/risk/regime.go:16-21`、`pkg/risk/volatility.go:16-23`），无请求级可变状态 ⇒ 并发只读安全；`TakeProfitChecker` 的 map 可变（`pkg/risk/take_profit.go:366`），**不在 `RiskManager` 里**。 |
| F2 | **`OrderIntent` / `Verdict` 只有一个 Go 使用方**；设计 §5 把它们画进 contracts，按 ADR-0001 今天不成立。 | `pkg/risk/interfaces.go:29/46`；`pkg/live/interfaces.go:34` 是 **core 内别名**；contracts 仓没有 risk 包（`tellurion-contracts:docs/API.md:134` 只登记 `domain.RiskManager`）。 | port 留 core；X3b 冻结的是 **wire JSON**（字段名 / 类型），不是 Go 类型。只有出现第二个 Go 使用方（本仓服务端也 import 同一 DTO）才按 ADR-0001 ① 迁 contracts。`TestOrderIntentFields`（`pkg/risk/interfaces_compliance_test.go:126`）用反射冻结字段集，加字段要同步它。 |
| F3 | **`risk_rules_hash` 的写入路径已就位（QT-63，2026-10-22），但今天恒为空。** | `pkg/backtest/provenance.go:194`（生产者显式填 `RiskRulesHash: ""`）、`:220`（映射进记录）；`pkg/storage/run_provenance.go:33`（`RunProvenanceRecord`）/`:56`（DDL）/`:86`（`SaveRunProvenance`）/`:126`（`GetRunProvenance`）；`pkg/storage/postgres.go:1189-1193`（Migration 056 收编）；`pkg/backtest/contracts/provenance.go:174`（字段）、`:216`（无条件 `c.Str` 进 canonical）。 | QT-42 ⑤「结果能记 `risk_rules_hash`」**不再依赖 QT-63**：表与写入路径都已落，只差 X3b 把空串换成真值。空串**也参与** canonical hash ⇒ 开启风控分量后，同参 run 的 `run_spec_hash` 会变——这是**语义变更**（hash 域不变），不是 bug；跨开启点的 hash 不可比，双跑对拍必须锁同一快照。 |
| F4 | **`/api/risk/*` 在 core :8085 已被四条进程内端点占用**。 | `cmd/analysis/handlers_risk.go:50-57`；`docs/openapi.yaml:1605/1629/1657/1696`；这些端点背后是 in-process `*risk.RiskManager`（`handlers_risk.go:24-32`），回测引擎从不 HTTP 调自己。 | 本仓 HTTP 面若也挂 `/api/risk/*`，升格时**必须**按最长前缀划所有权；本仓**永不接管**这四条（它们是进程内 RiskManager 的调试面）。命名待裁 **R1**。 |
| F5 | **M5：六仓 ≠ 六进程；U7（compliance / alert 归属）未裁**。 | `tellurion:docs/design/qt-decoupling/repo-inventory.md` §1.2 与 §3.1 U7。 | 本仓今天只到「仓内实现 + core 侧 port」，**不升格容器**；C 组是升格时的目标形状。监控三端点（`monitor/exposure` / `alerts` / `evaluate`）**形状不冻结**——取决于 U7（六个检测器留 core vs 迁出）。熔断指令的**执行方**同样等升格 ADR。 |

### 9.2 A 组 · 进程内 port（同步，零网络）

| 成员 | 今天（签名 + 取证） | 契约位 | 错误语义 | 版本策略 |
|---|---|---|---|---|
| `domain.RiskManager` | 三方法（`pkg/domain/types.go:204`）：`CalculatePosition(ctx, Signal, *Portfolio, *MarketRegime, float64, []OHLCV) (PositionSize, error)` · `DetectRegime(ctx, []OHLCV) (*MarketRegime, error)` · `CheckStopLoss(ctx, []Position, map[string]float64) ([]StopLossEvent, error)` | `contracts/domain`（S1 随 QT-05 整包迁；`tellurion-contracts:docs/API.md:134` 登记为 stable） | 接口本身不带哨兵；实现见下三行 | **frozen**：签名逐字对齐；删 / 改方法 = breaking（所有实现红） |
| `RiskManager.CalculatePosition` | `pkg/risk/manager.go:86` | core（实现留 core） | 空 `symbol` / nil `portfolio` ⇒ `errors.InvalidInput`（`:95-99`）；**ATR 不足回落 2% 默认值**（`:133-165`）、**`currentPrice <= 0` 被改写成 `100.0`**（`:120-122`）——后者与「缺失不得变成合理值」冲突，X3 适配时**不得**照搬进快照路径；批量版对空 `symbol` **静默跳过**（`:209-211`），与单条版报错不一致 | 行为变更（含默认值）走 MINOR + CHANGELOG；`RiskManagerConfig` 默认值不变（QT-42 ④） |
| `RiskManager.DetectRegime` | `pkg/risk/manager.go:299` | core | 样本 < `SlowMAPeriod` ⇒ `errors.DataQuality`（`:306-311`） | 同上 |
| `RiskManager.CheckStopLoss` | `pkg/risk/manager.go:331` | core | 空持仓 ⇒ `(nil, nil)`；空 `prices` ⇒ `InvalidInput`（`:336-341`）；ATR 用现价 2% 近似（`:345-355`）——**已知口径限制**，不是精确 ATR | 同上 |
| `RiskEngine.CheckOrder` | `pkg/risk/interfaces.go:69`；**零实现**：`StandardRiskEngine` 是 panic stub（`:93`），`RiskManager` 未实现它；零调用（F1） | core（port 留 core；本仓服务**不实现**它） | **本表冻结**：`err != nil` ⇒ 调用方必须按**拒绝**处理；禁止 `(Verdict{Allowed:true}, err)`；`ctx` 取消 / 超时同样拒绝。实现单必须用测试钉住这三条，且输入校验（空 `OrderID` / `Symbol`、`Qty <= 0`）一律拒绝 | 新增方法 = breaking（所有 `RiskEngine` 实现红）；`CheckOrder` 签名在本表冻结后改 = breaking + ADR |
| `Engine.SetRiskManager` / `WithRiskManager` | `pkg/backtest/engine.go:1843` / `pkg/backtest/options.go:51`，参数是**具体类型** `*risk.RiskManager` | core 内部，不属跨仓契约 | — | 改成接口注入是 core 内部 breaking（QT-42 实现单内做），不影响 A 组方法签名 |
| `TakeProfitRule` / `RuleBinding` | `pkg/risk/take_profit.go:70` / `:459` | core（今天只有 core 用；规则**参数**由快照下发，Go 接口不下发） | `Evaluate(pos, price) (TakeProfitAction, bool)`；`bool=false` = 不触发；规则构造错在 `NewTieredTakeProfit` 返回 error（`:250`） | `Name()` 是规则类型标识，**改名 = breaking**（快照里存这个字符串）；新增类型 = MINOR，但旧读者必须 **fail-closed 拒收未知类型**，不许忽略 |

### 9.3 B 组 · 值对象与 wire DTO

| 符号 | 今天（字段 / 取证） | 契约位 | 错误语义 | 版本策略 |
|---|---|---|---|---|
| `OrderIntent` | `pkg/risk/interfaces.go:29`：`OrderID`（调用方生成的幂等键）、`RunID`、`Symbol`、`Side`、`OrderType`、`Qty`、`Price`、`Ts`（回测 = 数据时间，实盘 = 墙钟）；`pkg/live/interfaces.go:34` 是别名 | **core-only**（F2）；X3b 只冻结 **wire JSON**（snake_case，字段名如上） | 字段缺失由 `CheckOrder` 判：`OrderID` / `Symbol` 空、`Qty <= 0` ⇒ 拒绝；不允许「缺省放行」 | 加字段 = MINOR，但**位置化字面量会红**（QT-19 ③）；改名 / 删字段 = breaking；JSON tag 改名 = wire breaking |
| `Verdict` | `pkg/risk/interfaces.go:46`：`Allowed bool` / `Reason string`；零值必须拒绝（`pkg/risk/interfaces_compliance_test.go:115` `TestVerdictFailClosed`） | 同上 | 零值 = 拒绝（**冻结**）；`Allowed=true` 时 `Reason` 可空 | 结构 **frozen**；加字段 = MINOR，且不得把零值变成放行 |
| `RuleSnapshot` | **本仓已实装 wire 形状**（X3b，`internal/rule/snapshot.go`）：`Snapshot{risk_rules_hash, rules[], created_at}`；`risk.rule_snapshots` DDL 已落 | 目标 `tellurion-contracts/risk`（届时应已有 core + 本仓两个使用方） | 解析失败 / hash 不匹配 ⇒ 拒绝开跑（不回落 latest）；`rule.ParseBundle` 双头 derive 校验 | 内容寻址：任一规则内容变 ⇒ 新 hash；**hash 算法变更 = 域标签 v2（全历史 hash 作废）**。样板 = `FactorDefinition.VersionHash()`（`pkg/domain/factor_def.go:151`：canonical JSON 的 SHA-256，加载时重新 derive 校验，见 `pkg/storage/factor_definition.go:94-97`） |
| `RuleEvent`（目标） | **本仓已实装 wire 形状**（X3c，`internal/event`）：`{run_id, order_id, event_seq, symbol?, verdict?, reason?, payload?, ts}`；`risk.risk_events` DDL 已落；未知字段保留在落库原文 `raw` | 目标 `tellurion-contracts/risk`（同 `RuleSnapshot`） | 去重键 `run_id + order_id + event_seq`；批量部分失败必须**逐条**报（207 `failed[{index, code, message}]`），不许全量 500 / 全量重发 | 加字段 = MINOR；未知字段**不得丢弃**（原文进 `raw` JSONB） |
| `BreakerCommand`（目标） | **本仓已实装 wire 形状**（X3d，`internal/breaker`）：`{breaker_id, action, scope, reason?, ts}`；动作冻结枚举 `halt` / `cancel_all`；`risk.circuit_orders` DDL 已落 | 同上 | 幂等键 `breaker_id`；同键同载荷 ⇒ `already_applied`；同键不同载荷 ⇒ **409 `idempotency_conflict`**（不许静默成功）。载荷身份 = action + scope + reason，`ts` 不参与 | 幂等键语义 frozen；新增动作枚举 = MINOR（旧执行方 fail-closed 拒收未知动作） |
| `risk_rules_hash`（目标值） | 定义在 `pkg/backtest/contracts/provenance.go:174`，无条件进 canonical（`:216`）；QT-63 已落表与写入路径，但生产者今天写空串（F3） | 已属 contracts（QT-03 冻结域） | 空串 = 未登记；**不得用空串冒充「无风控」** | SHA-256 hex（64 字符）；域标签变更 = breaking |

### 9.4 C 组 · 服务对外 HTTP 面（升格后）

> **鉴权**：按 core `QT-50 ④`（JWT + RBAC）统一，本仓**不另定**鉴权契约。目标：写侧（`POST/PUT /rules`、`approve`、`POST /breakers`）需 operator 及以上；读侧（`resolve`、`snapshots`）需 service 角色。**默认 fail-closed**：无 token / 过期 / 角色不足即拒绝（`401 unauthenticated` / `403 forbidden`）；loopback-only insecure 仅限显式本地开发配置（同 QT-50 ④）。
> **wire 契约落点**：core `docs/openapi.yaml`（QT-50 ③-③）。今天它只有四条**进程内** risk 端点（F4），**没有**下表 12 条；下表在升格前是目标草案，**不复制进 openapi**（`repo-inventory.md` §3 与 `tellurion-ui:AGENTS.md` §3 同条禁令）。落 openapi 属 QT-50 / 升格单。
> **错误码**：与 `docs/api-schema.md` §2 双向一一对应，本表不新增第四套词表。

| 端点 | 目标签名（请求 → 响应） | 错误语义 | 幂等 / 版本策略 |
|---|---|---|---|
| `POST /api/risk/rules` | `{rule_id, scope, type, params, description?}` → `201 {rule_id, revision, status:"draft", version_hash}` | `400 invalid_rule_type` / `400 invalid_params`；`409 idempotency_conflict`（同 `rule_id` 不同内容） | `rule_id` 由调用方给，是幂等键：同键同内容重放 → `200` + 现有 revision，不新建。加字段 = MINOR |
| `PUT /api/risk/rules/{id}` | `{base_revision, params, description?}` → `200 {rule_id, revision, status:"draft", version_hash}` | `404 rule_not_found`；`409 revision_conflict`（`base_revision` 不是当前 head）；`409 rule_deprecated` | 非幂等（每次产生新 revision）；`base_revision` 防丢更新。改参数语义 = MAJOR / ADR |
| `POST /api/risk/rules/{id}/approve` | `{revision}` → `200 {rule_id, revision, status:"active", version_hash}` | `404 rule_not_found`；`409 revision_not_draft` | 同 revision 重复批准 → `200` + 同一 `version_hash`（幂等）；状态机之外无第二条路 |
| `GET /api/risk/rules?scope=&active=true` | → `200 {rules:[{rule_id, revision, status, version_hash}], count}` | `400 invalid_params` | 只读可重试；路径只增不删 |
| `GET /api/risk/snapshots/{hash}` | → `200 {risk_rules_hash, rules, created_at}` | `404 snapshot_not_found` | 强幂等：同一 hash 两次拉取**字节一致**；缓存按 hash、永不失效。✅ 已实装（X3b） |
| `POST /api/risk/resolve` | `{hash}` **或** `{ruleset:"latest"}`（二选一）→ `200 {risk_rules_hash, snapshot}` | `400 invalid_params`（两者同时给 / 都不给）；`404 no_approved_rules`；`404 snapshot_not_found`（**不回退 latest**） | 只读可重试；`latest` 只解析到**当前 approved**，不越过审批。✅ 已实装（X3b） |
| `GET /api/risk/snapshots/{hash}/bundle` | → `200`（canonical snapshot JSON） | `404 snapshot_not_found`；内容重算 hash ≠ 路径 hash ⇒ `500` + 告警（缓存串号） | 强幂等；原地改内容是**绝对禁止项**（内容变 ⇒ 必然是新 hash）。✅ 已实装（X3b；`rule.ParseBundle` 双头 derive 校验） |
| `POST /api/risk/events` | `{events:[RuleEvent...]}` → `200 {accepted, duplicates}` 或 `207 {accepted, duplicates, failed:[{index, code, message}], code:"partial_failure"}` | `400 invalid_params`（整批不可解析 / 空数组）；`207 partial_failure`（逐条） | 去重键 `run_id + order_id + event_seq`；重试只重发 `failed`。加字段 = MINOR。✅ 已实装（X3c） |
| `POST /api/risk/breakers` | `{breaker_id, action, scope, reason, ts}` → `200 {breaker_id, status:"applied"\|"already_applied"}` | `400 invalid_params`（未知 action / scope）；`409 idempotency_conflict`（同键不同载荷） | 幂等键 `breaker_id`；载荷身份 = action + scope + reason（`ts` 不参与）；同键同载荷 → `already_applied`。✅ 已实装（X3d） |
| `GET /api/risk/monitor/exposure` | ⛔ **F5 / U7 未裁，形状不冻结** | — | 今天 core 的对应面不是它：`/api/compliance/*` + `/api/alerts/*`（`cmd/analysis/handlers_compliance.go:94-104`、`cmd/analysis/handlers_alert.go:138-142`）；迁移边界等 U7 |
| `GET /api/risk/monitor/alerts` | ⛔ 同上 | — | 同上 |
| `POST /api/risk/monitor/evaluate` | ⛔ 同上 | — | 同上 |

### 9.5 D 组 · 写入口径（事件 / 熔断 / 审计 / 留痕）

| 写入点 | 今天 | 目标口径 | 幂等 / 失败语义 |
|---|---|---|---|
| `risk.rule_snapshots` | 🔶 已实装（X3b）：Memory / PG Store 读写 + DDL 已落（启动时执行）；写入 / 读取 / ParseBundle 三处重新 derive hash 校验 | 内容寻址、不可变；写入时以重新 derive 的 hash 为准（样板 `pkg/storage/factor_definition.go:94-97`：库里存的 hash 与 derive 值不符即拒） | 同 hash 重复写入 = no-op；同 hash 不同内容 = **fail-loud**（不许 `ON CONFLICT DO NOTHING` 吞掉） |
| `risk.risk_events` | 🔶 已实装（X3c）：`internal/event` + Memory/PG 写入 + DDL 已落（启动时执行） | 唯一 owner = 本仓；去重键 `(run_id, order_id, event_seq)`；`raw` 保留事件原文 | 事件回传允许延迟、**不允许丢**；整批原子写入；部分失败 → `207` + 逐条明细 |
| `risk.circuit_orders` | 🔶 已实装（X3d）：`internal/breaker` + Memory/PG 写入 + DDL 已落 | `breaker_id` UNIQUE；记录下发与执行状态（今天只写 `applied`） | 同键同载荷 → `already_applied`；同键不同载荷 → `409` |
| `risk.audit_logs` | 待建（**新表**） | 规则 / 审批 / 熔断的人操作留痕 | **不迁、不复用** core 的 `audit_logs`：后者是 ADR-017 鉴权访问审计（`pkg/storage/postgres.go:335` DDL、`pkg/auth/auth.go:492` `RecordAudit` 写入），语义不同（`docs/db-schema.md` §1 注） |
| `quant.run_provenance.risk_rules_hash` | 表与写入路径已落（QT-63），生产者写空串（F3） | X3b 把空串换成真值；只存「输入身份」，不存 status | 同一 `run_id` 写不同 hash ⇒ fail-loud（QT-63 ④-⑦） |

### 9.6 E 组 · 版本策略汇总（QT-19 口径）

- **frozen（改 = breaking + ADR）**：`RiskEngine` 四方法签名；`OrderIntent` / `Verdict` 的 wire 字段名与类型；`Verdict` 零值 = 拒绝；`risk_rules_hash` 的算法与域标签。
- **stable（改 = MINOR + CHANGELOG；语义变更 = MAJOR / ADR）**：既有端点路径与错误码；`RuleSnapshot` / `RuleEvent` / `BreakerCommand` 的字段集（加字段 MINOR，改名 / 删字段 breaking）。
- **evolving（⛔ 冻结前不实现）**：`monitor/*` 三端点（U7）；熔断指令的**执行方**与 ACK（升格 ADR）。
- **规则类型枚举**：新增类型 = MINOR，但**旧读者必须 fail-closed 拒收未知类型**；改既有类型的语义 = breaking（历史 `risk_rules_hash` 的可比性失效）。
- **快照格式**：新格式必须保留旧格式读取器（否则历史回测不可复现，`docs/api-schema.md` §4）。
- **路径**：只增不删；废弃走 `Deprecated` 头 + 一个 MINOR 周期。

### 9.7 待裁（R 组）与移交

| # | 事项 | 当前状态 | 建议 / 影响 |
|---|---|---|---|
| R1 | `/api/risk/*` 前缀所有权（F4） | core 已有四条进程内端点；本仓目标面同前缀 | 建议：本仓只拥有 `rules` / `snapshots` / `resolve` / `events` / `breakers` / `monitor`；core 四条**永不迁入**；网关按最长前缀转发。拍板时机 = 升格 ADR |
| R2 | 事件写入路径：服务侧异步收集 vs 内核回调 | `docs/db-schema.md` §5 未定稿 | 影响 C 组 `POST /events` 的重试与 `207` 语义；X3c 开工前拍 |
| R3 | compliance / alert 归属（U7） | core `repo-inventory.md` §3.1 待裁 | 监控三端点形状冻结的前置；X3c 开工前拍 |
| R4 | 鉴权角色矩阵 | QT-50 ④ 统一面未落定 | 本文只冻结「默认 fail-closed，无凭证不服务」；具体角色名与 401/403 body 随 QT-50 对齐 |
| R5 | `CheckOrder` 的输入校验细表 | 零实现（F1） | 本表冻结错误语义与总判据；逐字段校验清单在实现单里用测试钉住，不在此处预写 |

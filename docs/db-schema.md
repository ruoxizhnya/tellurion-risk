---
status: active
last-verified: 2026-10-25
verified-by: QT-42 X3c 的 core 腿——risk_events 有了生产者（告警流）；同批登记「事件回传不落审计行 / 无 actor 列」
---

# DB Schema — tellurion-risk

> **所有权声明**：本仓**拥有 `risk` schema 的 DDL**：规则定义、版本、快照、限额、裁决事件、熔断指令、审计。是这组表的唯一写者。
> 上级依据：`tellurion:docs/design/qt-decoupling/data-service-contract-and-ownership.md` §1 —— **DDL 只有一个 owner**，同一个 PG 实例、每服务一个 schema，将来拆库是"按 schema 搬走"而不是重写。
> 本文件是本仓表结构的**唯一真相源**：代码里的建表语句与它不一致时，**改代码**；要改表结构，先改本文件（结构级变更还要立 ADR）。

## 1. 归属表

| 表 | 内容 | 现状 |
|---|---|---|
| `risk.rule_defs` | 规则定义（类型 / 参数归属 / 适用范围），`head_revision` 指向当前 head，`active_revision` 指向生效版本 | ✅ DDL + Memory/PG Store（2026-10-24） |
| `risk.rule_revisions` | 版本追加 + 审批状态（draft→approved→active→deprecated）+ `version_hash` | ✅ 同上 |
| `risk.rule_snapshots` | run 前下发的规则快照（内容寻址；回测结果记 `risk_rules_hash`） | ✅ DDL + Memory/PG 读写（`internal/rule/snapshot.go` / `store.go` / `internal/pgstore`） |
| `risk.limits` | 账户 / 策略 / 标的级限额 | 🔶 DDL 已落（2026-10-24，仅建表；读写路径与端点未冻结） |
| `risk.risk_events` | 裁决事件留痕（run 后异步批量回传；去重键 `run_id + order_id + event_seq`，`raw` 保留原文）；监控的**唯一读源**（敞口 / 告警 / evaluate） | ✅ DDL + Memory/PG（2026-10-24，X3c；监控读取 2026-10-24） |
| `risk.circuit_orders` | 熔断指令下发记录（幂等键 `breaker_id`；载荷身份 action + scope + reason） | ✅ DDL + Memory/PG（2026-10-24，X3d） |
| `risk.audit_logs` | **规则与审批的操作审计**（谁改了哪条规则、谁批的） | 🔶 DDL 已落（**新表**，与现 `audit_logs` 无关、不复用——后者是 ADR-017 鉴权访问审计（`tellurion:pkg/auth` 写入），留 core） |

**约束（2026-10-23 落地）**：

- `rule_defs`：`PRIMARY KEY (rule_id)`；`CHECK (scope IN (...))` / `CHECK (type IN (...))` 与 Go 侧冻结枚举**同源**；
  `CHECK ((status = 'active') = (active_revision IS NOT NULL))` —— 定义级 status 只表达「有没有生效 revision」。
- `rule_revisions`：`PRIMARY KEY (rule_id, revision)` + `UNIQUE (rule_id, version_hash)`；
  `CHECK ((status IN ('approved','active','deprecated')) = (approved_at IS NOT NULL))` —— 未批准过的 revision 不得带批准时间。
- `audit_logs`：`seq BIGSERIAL PRIMARY KEY`；`action CHECK IN ('create','update','approve','deprecate')`；**不设更新路径**（只追加）。
- `rule_snapshots`：`PRIMARY KEY (risk_rules_hash)`;
  `CHECK (risk_rules_hash ~ '^[0-9a-f]{64}$')`;
  `CHECK (jsonb_typeof(rules) = 'array' AND jsonb_array_length(rules) > 0)` —— 快照至少含一条生效规则。
  应用层双头护栏：写入与读取都重新 derive hash 比对（`ErrSnapshotMismatch` fail-loud）；
  同 hash 重复写 = no-op，同 hash 不同内容 = 拒收（不许静默覆盖，docs/ARCHITECTURE.md §9.5）。
- `limits`（2026-10-24 建表）：`PRIMARY KEY (scope, subject, limit_key)`；
  `CHECK (scope IN (...))` 与 Go 侧冻结枚举同源；`CHECK ((scope = 'global') = (subject = ''))` ——
  `global` 无主体，其余 scope 必须给主体（账户 id / 策略 id / 标的代码）。
  本切片只建表，读写路径与端点未冻结（登记在 `docs/TASKS.md` QT-42 未落项）。
- `risk_events`（2026-10-24，X3c）：`PRIMARY KEY (run_id, order_id, event_seq)` —— 去重键即主键；
  `CHECK (event_seq >= 0)`；`raw JSONB NOT NULL` 保存事件原文（未知字段不丢），
  `payload JSONB` 是调用方显式给的审计载荷；`received_at` 由服务端盖，`event_ts` 是调用方时间。
  写入按批事务：任何一条失败整批回滚；主键冲突 = duplicate（`ON CONFLICT DO NOTHING` 计数，不算失败）。
  **监控读取（2026-10-24）**：按 `event_ts` 时间窗读取，新增索引
  `risk_events_ts_idx ON risk.risk_events (event_ts DESC)`；告警投影只认 `verdict='alert'` 且 `payload`
  合法（`rule` / `severity` / `message`），非法投影计入 `invalid_events` 而不是静默丢弃。
- `circuit_orders`（2026-10-24，X3d）：`PRIMARY KEY (breaker_id)` —— 幂等键即主键；
  `CHECK (action IN ('halt','cancel_all'))` 与 Go 侧冻结枚举同源；`scope` 复用规则的冻结枚举；
  `status` 今天只写 `'applied'`（执行方 ACK 状态待升格 ADR）；`received_at` 与 `command_ts` 分开存。
- `version_hash` 由 `rule_id + revision + scope + type + 规范化 params` 派生（域 `tellurion.risk_rule_version_hash.v1`），
  不含 `description` / `status` / 时间（见 `docs/adr/0002-规则版本-hash-与审批状态机的裁决.md`）。

**红线**：`risk` schema 只有本仓能写；内核与实盘只经 port 读快照、经端口回传事件。

> **两个 "audit" 不是一回事**（容易混）：
> - `audit` **schema**（`audit.message_log`）＝ **事件存储**，由 core 的 `pkg/eventstore` / `pkg/msgbus` 写，用于消息重放；**不出仓**（repo-inventory U4）。
> - `risk.audit_logs` ＝ **人操作审计**（规则变更 / 审批 / 熔断指令），由本仓写。
> 不要因为都叫 audit 就把它们合并——一个是"系统发生了什么"，一个是"人做了什么"。

## 2. 只读输入（不属于本仓，禁止写入）

| 数据 | 真实 owner |
|---|---|
| 订单 / 成交 / 持仓 | core（实盘运行域） |
| 回测结果 | core / `run` |
| 账户资金 | 券商柜台（经 broker 网关） |

本仓**不持有**订单与持仓真相 —— 它只裁决"这单能不能下"。

## 3. 红线

1. **同步裁决不出仓**：`CheckOrder` 永不走 HTTP（每笔多一次网络跳 = 实盘不可接受）。
2. **规则必须版本化**：任何生效变更都产生新 `version_hash`；回测结果记 `risk_rules_hash`。
3. **快照内容寻址、永不失效**：`/snapshots/{hash}` 的缓存按 hash，不按时间。
4. **熔断指令幂等**：重复下发不重复执行（幂等键）。
5. **审批不可绕过**：`draft → approved → active` 状态机之外没有第二条路。

## 4. 建表与迁移

**现状（必须先知道）**：core 的迁移是「**启动时按序执行一组幂等 DDL**」——唯一真相是 `pkg/storage/postgres.go` 的 `migrationStatements()` 数组（`migrate()` 在服务启动时逐条跑）；**没有版本表、没有 down、没有执行记录**（有意取舍，理由见 core 的 `migrations/README.md`）。`migrations/`（14 个）与 `docs/migrations/`（25 个）里的 `.sql` 是**历史镜像，不执行**——改它们不会改库。

**本仓约定**（从建仓第一天执行）：

- **执行机制已定（取向 ①，2026-10-23）**：Go 数组 + 启动执行、幂等、无版本表 —— 语句本体在
  `internal/rule/ddl.go` 的 `migrationStatements`，由 `rule.Migrations()` 交出副本。
  **执行入口（2026-10-24）**：`internal/pgstore.Migrate` 按序执行该数组；`cmd/riskservice` 在 DSN 非空时
  启动即 Ping → Migrate → 装配 PG Store（空 DSN 退回 Memory 并告警）。
  取向 ② 的重估条件不变（服务数 ≥3 或出现"必须先降级"需求）。
- 不另建 `migrations/*.sql`：本仓 DDL 的唯一真相是 `internal/rule/ddl.go` 的 Go 数组；`.sql` 会把真相劈成两份。
  （本节原先的「迁移文件只放 `migrations/`」随取向 ① 作废。）
- **只前滚**：已合并的迁移不改；修错靠新增一条。
- 每条必须可重入（`IF NOT EXISTS` / `IF EXISTS`），且能在空库上一次跑完。
- 跨 schema 取数只走**只读视图或 API**；出现 `join` 别人的表就是边界腐化的第一信号。
- 结构变更的同步义务：本文件 + `docs/ARCHITECTURE.md` 的「数据归属」节必须同一次提交里更新。

## 5. 已裁决

- **迁移取向 ①（2026-10-23）**：Go 数组 + 启动执行、幂等、无版本表（见 §4）。
- **rule_defs 双指针（2026-10-23）**：`head_revision`（编辑头）+ `active_revision`（生效版本）分开存。
  新建 draft 修订**不清空** `active_revision`：runtime 仍应拿到当前生效版本，直到新版本被批准。
  定义级 `status` 因此只表达「有没有生效 revision」（`active` / `draft`），**head 自身的 draft/active 由 revision 行表达**
  （API 列表的 `status` 取 head revision 的状态）。裁决落 `docs/adr/0002-规则版本-hash-与审批状态机的裁决.md`。
- **`version_hash` 域（2026-10-23）**：`tellurion.risk_rule_version_hash.v1`，本仓暂为唯一 owner
  （contracts 的 canonical 域表没有 risk 域）。
- **`risk_rules_hash` 域（2026-10-23，X3b）**：`tellurion.risk_snapshot_hash.v1`，同样本仓暂为唯一 owner。
  由**排序后的 active revision 内容**（rule_id / revision / scope / type / 规范化 params / version_hash）派生，
  **不含 `created_at`**——同一内容永远同一 hash，缓存按 hash 永不失效；hash 算法变更 = 域标签 v2，全历史作废。
- **审计表不复用 core 的 `audit_logs`（2026-10-23）**：`risk.audit_logs` 是新表，只记人与规则/审批的操作；
  core 的 `audit_logs` 是 ADR-017 鉴权访问审计，两者语义不同，不做迁移合并。
- **PG 事务边界（2026-10-24）**：Service 的读-改-写全部经 `Store.Tx`；PG 事务内 `GetDefinition` 用
  `SELECT ... FOR UPDATE` 行锁，Memory 用「全量状态快照 + 失败还原」模拟同一回滚语义。
  Service 不再持有进程内互斥锁。裁决见 `docs/adr/0003-pg-store-与事务边界.md`。
- **事件写入路径定为服务侧批量回传（2026-10-24，X3c）**：内核 / 实盘在 run 后调 `POST /events`，
  去重键 `(run_id, order_id, event_seq)`；未知字段保留在 `raw`，不丢原文。
  R2 的「服务侧异步收集 vs 内核回调」按此落地为**批量回传 + 服务端原子落库**。
- **监控读取口径定为 `risk_events` 单源（2026-10-24，ADR-0004）**：`monitor/exposure` / `alerts` / `evaluate`
  只读 `risk.risk_events`；告警 = `verdict='alert'` 的事件投影；检测器不搬出 core，`evaluate` 只做报表。
  敞口缺 `notional` / `qty+price` 时计入 `unpriced_events`，**不补零**；超过 5000 条事件拒绝而不是截断。
- **熔断载荷身份不含 `ts`（2026-10-24，X3d）**：`breaker_id` 是幂等键，载荷身份 = `action + scope + reason`。
  `command_ts` 是投递时间，重试可重新生成；把它算进载荷身份会让正常重试变成 409。
  同键不同 `action/scope/reason` → 409（不许静默成功）。

## 6. 待定

- **审计 actor 来源（2026-10-25 已落）**：鉴权挂在全部端点（ADR-0005），`actor` 从 claims 取 `username`
  （缺失退 `subject`）；显式 open-access（无 secret + 回环）记常量 `open-access`，**不再写空串**。
- **`risk.limits` 的读写路径**：表已建，但限额的读取口径（快照附带 / 独立端点）与写入端点未冻结（R2 相关）。
- **事件监控聚合（读取侧）**：~~随 U7 未裁~~ ✅ 2026-10-24（ADR-0004）——三端点只读 `risk_events`。
  生产侧接线 ✅（2026-10-25）：core 的告警按 `POST /events` 回传（`verdict:"alert"`、
  `run_id="core:alert-loop"`、`order_id=Alert.ID`、`event_seq=0`；落点
  `core:cmd/analysis/wiring/risk_events.go`，开关 `risk_service.events_enabled` 默认关）。
  仍未落：外部推送通道（webhook / 消息平台）；**事件回传不落审计行** —— 本表无 actor / account 列，
  `actorOf` 只作用于规则三端点（票面「actor 取自 claims」指的是 `risk.audit_logs`）。
- **熔断执行方与 ACK**：`circuit_orders.status` 今天只写 `'applied'`；谁执行、如何回报 `acknowledged/failed` 随升格 ADR（R3 同源）。

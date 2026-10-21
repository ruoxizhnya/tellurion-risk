---
status: active
last-verified: 2026-10-20
verified-by: 功能边界固化（三份契约：db / api / dataflow）
---

# DB Schema — tellurion-risk

> **所有权声明**：本仓**拥有 `risk` schema 的 DDL**：规则定义、版本、快照、限额、裁决事件、熔断指令、审计。是这组表的唯一写者。
> 上级依据：`tellurion:docs/design/qt-decoupling/data-service-contract-and-ownership.md` §1 —— **DDL 只有一个 owner**，同一个 PG 实例、每服务一个 schema，将来拆库是"按 schema 搬走"而不是重写。
> 本文件是本仓表结构的**唯一真相源**：代码里的建表语句与它不一致时，**改代码**；要改表结构，先改本文件（结构级变更还要立 ADR）。

## 1. 归属表

| 表 | 内容 | 现状 |
|---|---|---|
| `risk.rule_defs` | 规则定义（类型 / 参数 / 适用范围），带 `version_hash` | 待建（沿用 `factor_definitions.version_hash` 样板） |
| `risk.rule_revisions` | 版本追加 + 审批状态（draft→approved→active→deprecated） | 待建 |
| `risk.rule_snapshots` | run 前下发的规则快照（内容寻址；回测结果记 `risk_rules_hash`） | 待建 |
| `risk.limits` | 账户 / 策略 / 标的级限额 | 待建 |
| `risk.risk_events` | 裁决事件留痕（`OrderIntent` 注释已指向 `quant.risk_events`，**表未建**） | 待建，迁入 `risk` |
| `risk.circuit_orders` | 熔断指令下发记录（幂等） | 待建 |
| `risk.audit_logs` | **规则与审批的操作审计**（谁改了哪条规则、谁批的） | 待建（**新表**，与现 `audit_logs` 无关、不复用——后者是 ADR-017 鉴权访问审计（`tellurion:pkg/auth` 写入），留 core） |

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

- 迁移文件只放 `migrations/`，命名 `<序号>_<动作>.sql`，**本仓内**递增（不与别的仓共享序列，也不复用号）。
- **执行机制随第一个建表单定，二选一（不能"都不选"）**：
  1. **core 式**：Go 里的 `migrationStatements()` 数组 + 启动执行，幂等、无版本表——与 core 一致、运维零新增；
  2. **版本化**：引入迁移库 + `schema_migrations` 表（可 down、有执行记录）——服务变多后更稳，但要一次性 baseline。
  **取向**：X1~X3 期间优先 ①（少一层依赖），服务数 ≥3 或出现"必须先降级"的需求时重估 ②。
- **只前滚**：已合并的迁移不改；修错靠新增一条。
- 每条必须可重入（`IF NOT EXISTS` / `IF EXISTS`），且能在空库上一次跑完。
- 跨 schema 取数只走**只读视图或 API**；出现 `join` 别人的表就是边界腐化的第一信号。
- 结构变更的同步义务：本文件 + `docs/ARCHITECTURE.md` 的「数据归属」节必须同一次提交里更新。

## 5. 待定

- `risk_events` 的写入路径（服务侧异步收集 vs 内核回调）未定稿，见 design §4 与 ADR-D20。

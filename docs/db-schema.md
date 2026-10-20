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
| `risk.audit_logs` | 操作审计 | 从现 `audit_logs` 迁入 |

**红线**：`risk` schema 只有本仓能写；内核与实盘只经 port 读快照、经端口回传事件。

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

**现状（必须先知道）**：core 今天**没有迁移执行器**——建表靠 `pkg/storage/*.go` 里的内联 `CREATE TABLE IF NOT EXISTS`，另有 `migrations/`（14 个）与 `docs/migrations/`（25 个）两处**手工执行**的 SQL。这是拆分服务时必须先收口的债，不要在本文假设某个执行器已经存在。

**本仓约定**（从建仓第一天执行）：

- 只用 `migrations/` 一处，命名 `<序号>_<动作>.sql`，**本仓内**递增（不与别的仓共享序列，也不复用号）。
- **只前滚**：已合并的迁移文件不改；修错靠新增一条。
- 每条必须可重入（`IF NOT EXISTS` / `IF EXISTS`），且能在空库上一次跑完。
- 跨 schema 取数只走**只读视图或 API**；出现 `join` 别人的表就是边界腐化的第一信号。
- 迁移执行器与 applied 版本表随**第一个真正需要建表的单**一起落地；落地前本仓不产生"半迁移"。
- 结构变更的同步义务：本文件 + `docs/ARCHITECTURE.md` 的「数据归属」节必须同一次提交里更新。

## 5. 待定

- `risk_events` 的写入路径（服务侧异步收集 vs 内核回调）未定稿，见 design §4 与 ADR-D20。

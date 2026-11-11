# AGENTS — tellurion-risk

> 本文件是**本仓**的唯一入口。判据：一个 agent 只 checkout 本仓，就能知道「怎么构建、边界在哪、依赖哪版契约、活在哪、禁止什么」，**不需要读别的仓的源码**。

## 1. 这是什么仓

风控规则的**真相源**：规则定义、限额表、聚合监控与熔断。**同步裁决（`RiskPolicy`）留在 core 进程内**。

**状态**：✅ 首批实装已收口（QT-42 X3a 规则侧 + X3b 快照侧 + PG Store 事务边界 + X3c 事件回传与监控 + X3d 熔断指令；G0–G4 与 `SVC-RISK-G1/G3/G4` 均于 2026-11-10 收口）。剩余 3 项 ⏸ 见 `docs/TASKS.md`（不属于本阶段目标）。
规则定义 / 版本 / 审批 / 审计 + 快照内容寻址 / `resolve` / `snapshots/{hash}` / `bundle` 三端点 /
进程内 `ParseBundle` 边界 + `POST /events`（批量、去重键、207）+ `POST /breakers`（幂等键、409）+
`monitor/exposure` / `alerts` / `evaluate` 三端点（**只读 `risk_events` 聚合/报表**，检测器不搬出 core；
**全部端点**过鉴权闸：未装配 Authorizer 时 503 拒绝而非放行）。
审计读 `GET /api/risk/audit`；`actor` 取自鉴权 claims（open-access 记 `open-access`），不再有空串；鉴权实现见 ADR-0005。
服务间角色 `service`（裁-17）只开 `POST /api/risk/resolve` 与 `POST /api/risk/events` 两条——core 代机器调本仓走它；`/audit` 与规则管理面**不开**（ADR-0006）。

**本仓今天的实现**（`internal/rule` / `internal/event` / `internal/breaker` / `internal/pgstore` /
`internal/httpapi` / `cmd/riskservice`）：规则 4 端点 + 快照 3 端点 + 审计 1 端点 + 事件 1 端点 + 熔断 1 端点 +
监控 3 端点 + 审批状态机 + 内容寻址 `version_hash` / `risk_rules_hash` + 审计 +
`ParseBundle`（进程内 RiskPolicy 输入形状）+ 规则参数字典（`internal/rule/params.go`，14 个标量的
字段名 / 单位 / 取值域；写侧严格、读侧只拒未知键 —— 见 `docs/db-schema.md` §7）+ 事件去重落库 +
熔断幂等落库 + `risk_events` 聚合读
（`event.Reader`，Memory/PG）；持久化有 `Memory` 与 PG（`internal/pgstore`，启动时执行
`rule.Migrations()` 幂等 DDL）；Service 的读-改-写经 `Store.Tx`（PG 行锁）完成，不再用进程内互斥锁；
鉴权走 kit `auth/httpadapter` + 本仓 policy 表（全端点 fail-closed）；
core 侧消费接线**两半都已接**（2026-11-09 `SVC-CORE-RISK-RETIRE`）：✅ `risk_rules_hash` 生产者（QT-66 —— run 提交时调
`POST /api/risk/resolve`，hash 写进 `quant.run_provenance`，空串只剩「本部署未接入」=
`not_configured` 一种合法含义）；✅ 限额装配 —— core `LoadRiskPolicy` 走 `POST /api/risk/resolve` 的内联 `snapshot`
→ `risk.PolicyFromSnapshot`（`tellurion:contracts/risk` 的 `Verify()` 重新 derive hash）。**消费方刻意不走本仓
`ParseBundle`**：CT-03 ④ 已把快照 Go 类型迁 `contracts/risk` v0.9.0，本仓 `ParseBundle` 保留为仓内边界与测试基准；
core 的 `risk_manager.*` 配置块已删、`risk_service.resolve_enabled` 出厂 `true`。
告警生产侧接线同日落地：core 的 6 个检测器 + `Fire` 进来的对账 / 双跑差异按 `POST /api/risk/events`
回传（`verdict:"alert"`），所以 `monitor/alerts` / `evaluate` 不再是结构性空集 —— 但生产侧开关
`risk_service.events_enabled` **默认关**，关着的部署里空集是设定。
`POST /events` 今天**不写审计行**（`risk.audit_logs` 的 `action` 枚举只允许规则动作），但事件自身的
调用方身份已落 `risk.risk_events.actor`（QT-80，2026-11-07：取自鉴权 claims，服务 token 落 `sub`；
请求体自报的 `actor` 不生效）。

**服务探针与请求治理（G1，2026-11-07）**：`GET /healthz`（进程存活，不读库）/ `GET /readyz`
（PG 可连 + `rule.Migrations()` 已执行；未注入检查 = 内存档，响应标 `store:"memory"`）两条**公开**端点，
不鉴权、不限流；业务端点按调用方身份（`username` → `subject` → `open-access` 共用一桶）令牌桶限流
（默认 50 rps / 突发 200），**鉴权先于限流**（401/403 不会被 429 顶替），超限 `429 rate_limited` + `Retry-After`。
真实 PG 断开/恢复与 `compose up --wait` 两条腿已于 **2026-11-10** 真跑（Docker 引擎 29.8.0 起来后 `docker compose up --wait` EXIT=0、pg/risk 双 healthy，`/readyz` 停 PG → 503、恢复 → 200），`SVC-RISK-G1` 收口。

**空集判别（G4，2026-11-07）**：`monitor/*` 三个响应都带 `configured`（必有键）：`false` = 未接入
（部署声明 `RISK_EVENTS_EXPECTED`，缺省 false，与 core 出厂 `events_enabled=false` 对齐）；`true` + 空 =
已接入但没触发。本仓看不见 core 的开关，所以它是**声明**不是探测；读失败（PG 不可达）走非 2xx，不降级成空集。
失败语义五类、core→risk 超时/重试口径见 `docs/api-schema.md` §2/§3.1；运维步骤见 `docs/runbook.md`。

**留在 core 的实现**：`core:pkg/risk`（`RiskManager` / `RegimeDetector` / `StopLossChecker` / `CheckOrder`）+ `core:pkg/compliance`（异常交易 / 减持 / 大额报送）+ `core:pkg/alert`（**6 个检测器 + 进程内 `alertLoop`**）。
**U7 已裁（2026-10-24）**：告警的**通道编排与报送**归本仓（`monitor/exposure` / `alerts` / `evaluate` 三端点）；**检测器不搬出 core**——逐 bar / 逐笔裁决仍在本进程内，禁止远程调用。

## 2. 构建 / 测试（必须单仓可跑）

```bash
go build ./...
go vet ./...
go test ./...
```

- 这三条是 R1/R2 的判据：**单独 checkout 就能绿**。
- 若某一步需要别的仓的源码目录才过，那是**边界画错了**——停下来修边界，**不要**加 `replace ../xxx` 绕过。
- 本机同时改多个仓时可用 `go.work` 联调，但**它不入库**（`.gitignore` 已挡）。

## 3. 边界（硬约束）

**允许**

- 持有 `risk` schema 的 DDL 与规则版本
- 下发限额快照给 core 的进程内裁决层
- 聚合监控与熔断（跨 run 维度）

**禁止**

- 把逐笔 / 逐 bar 裁决做成远程调用（硬否决）
- import core 的实现包
- 持有订单/持仓真相（那是 core）

下面三条对所有仓都成立：

- **禁止** `replace` 指向仓外路径（单仓就不可构建了）。
- **禁止**提交 `go.work` / `go.work.sum`（本地产物；已在 `.gitignore`）。
- 新增第三方依赖前，先在第 4 节登记，再改 `go.mod`；没登记就加依赖 = 绕过评审。

## 4. 依赖的契约版本

| 依赖 | 版本 | 说明 |
|---|---|---|
| `github.com/ruoxizhnya/tellurion-contracts` | `v0.10.0`（2026-11-11 `RISK-CONTRACTS-SNAPSHOT-MIGRATION`：`Snapshot` / `SnapshotRule` / `Scope` / `RuleType` / `DomainSnapshotHash` / 四个跨仓哨兵改为别名 + `SnapshotHash` 委托 `Snapshot.Hash()`；`Bundle` / `ParseBundle` / `ForScope` 与参数字典留本仓） | `RiskPolicy` 相关 DTO 只认契约；形状唯一真相 = `contracts/risk`（ADR-040）。pin tag 不 pin 伪版本 |
| `github.com/ruoxizhnya/tellurion-kit` | `v0.2.0`（2026-10-25 从 `v0.1.1` 升到 `v0.1.4`，KT-11；2026-11-10 随 `KT-13` 升） | 鉴权机制：`auth`（token 签发 / 解析）+ `auth/httpadapter`（net/http 适配、启动闸、Policy）。本仓只写策略表，不另造 token 校验。**`v0.1.4` 起 `role=service` 的非数字 subject 不再被 401**——这是本仓 resolve/events 两条 service 档位真正可用的起点 |
| `github.com/jackc/pgx/v5` | `v5.7.5` ⚠️ **分叉已登记** | PostgreSQL 驱动。仅 `cmd/riskservice` 空白导入 `stdlib` 注册 `database/sql` 驱动；`internal/pgstore` 只用 `database/sql`，不直接依赖 pgx API（`pgconn` 仅用于唯一约束错误识别）。**别按别的仓那句「与 core 对齐」推断本仓**：2026-10-26 实测 core / kit / data / strategy / agent / orchestration / broker 七仓 = `v5.7.3`，**本仓 = `v5.7.5`**，analytics = `v5.11.0`。三档并存不是同一把刀，跨仓抄 `pgxpool` / `pgtype` 用法前先核目标仓的版本；统一到一档要单独立单，不在顺手 tidy 里做 |

> 跨仓依赖**只认版本号**（伪版本或 tag），不认源码路径。契约变更走 `tellurion-contracts` 的发布，不走"顺手改一下"。

## 5. 活在哪、文档在哪

- **本仓台账**：`docs/TASKS.md`（唯一）。**没有单号别开工。**
- 文档导航：`docs/README.md`；边界与依赖方向：`docs/ARCHITECTURE.md`；决策：`docs/adr/`；
  起停 / 审计 / 迁移重放 / 熔断解除 / 开关核对：`docs/runbook.md`。
- 跨仓背景（只读，不在本仓改）：`tellurion:docs/design/qt-decoupling/repo-topology-and-naming.md`、`tellurion:docs/TASKS.md`。

> 跨仓引用一律写 `repo:path` 纯文本，**不做可点击相对链接**——独立 checkout 时 `../../` 必断，R2 会永远红。

## 6. 文档真相源（改功能前先看这里）

本仓的「功能边界」由三份文件钉住，**它们是唯一真相源，不是参考读物**：

| 维度 | 文件 |
|---|---|
| 数据形状（我拥有哪些表、谁能写） | `docs/db-schema.md` |
| 接口形状（对外暴露什么、错误码、幂等） | `docs/api-schema.md` |
| 流向与顺序（数据怎么走、哪里 fail-closed） | `docs/dataflow.md` |

**变更触发是硬约束**（完整表见 `docs/README.md` §3）：改表 → 改 `db-schema.md`；改接口 → 改 `api-schema.md`；改链路 → 改 `dataflow.md`；改分层 → 改 `ARCHITECTURE.md` + 一条 ADR；删档/迁移/换工具 → 一条 ODR。PR 里代码动了而对应文档没动，应被打回。

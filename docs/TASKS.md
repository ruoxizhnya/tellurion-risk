---
status: active
last-verified: 2026-10-22
verified-by: M8 接口对表落盘（QT-42 前置 ② 完成）
---

# TASKS — tellurion-risk 未完成项

> 本仓**唯一**任务台账。跨仓任务只在 §二 留指针，**不镜像正文**（双写必然漂移）。
> 状态：`⬜ 待做` / `🔶 进行中` / `⛔ 阻塞（有未解除前置）` / `✅ 完成（附日期）`
> 每张单六字段（① 目标 ② 上下文 ③ 要求 ④ 约束与非目标 ⑤ 验收 ⑥ 边界），判据见 `tellurion:AGENTS.md` §8.5。
> 完成即标 ✅（附日期）并移入 `docs/archive/TASKS-YYYY-MM-DD-archived.md`；本文件只留未完成项。

---

## 一、本仓任务

> **约定变更（2026-10-21）**：原「占位仓不写正式六字段任务单」作废——X 线任务单已展开为本仓正式任务（见下）。M5/M8 开工前置不变；实现仍未开工，开工前仍须过前置两条。

### QT-42 · X3 风控服务（规则真相源 + 进程内 RiskPolicy）[P2] ⬜

> **迁入说明（2026-10-21）**：本单自 core 仓 `tellurion:docs/TASKS.md`「五、X 线」迁入（任务台账批量重分布：X 线单展开到各服务仓为正式六字段单，「占位仓不写正式任务单」旧约定同时作废）。六字段正文逐字保留、不改写；core 侧同号条目已改为指针。
>
> **M8 前置完成（2026-10-22）**：接口对表见 [`ARCHITECTURE.md`](ARCHITECTURE.md) §9。开工前先读 §9.1 的五条前置事实——`CheckOrder` **零实现零调用**、`risk_rules_hash` 写入路径已由 QT-63 落库但**恒为空**（等 X3b 填）、`/api/risk/*` 前缀已被 core 四条进程内端点占用、U7 未裁（监控半边形状不冻结）。本单**仍 ⬜**：对表是开工前置，不是实现。
> 对表自检（2026-10-22）：21 条 core `path:line` 按 HEAD `dbe4c68` 逐条命中；C 组 12 端点 / 13 错误码与 `api-schema.md` 双向一致；破坏验证三腿（改错行号 / 删一行端点 / 删一个错误码）各自红，还原后绿。

- **① 目标**：风控规则可版本化、可审计；回测与实盘调用**同一** RiskPolicy；结果能记 `risk_rules_hash`。
- **② 上下文**：`risk-service-design.md`（D20）；`risk.rule_defs`/`rule_revisions`/`rule_snapshots` 待建；`risk_events` 表未落地（`OrderIntent` 注释已指向）。
- **③ 要求**：① 规则表 + `version_hash`（复用 factor 样板）+ 快照下发 `/api/risk/snapshots/{hash}`；② 进程内 `RiskPolicy` 接口化（现状 `RiskManager` 已无状态，可直接适配）；③ `CheckOrder` **永不走 HTTP**；④ 熔断指令走异步；⑤ 结果记 `risk_rules_hash`（QT-03 分量）。
- **④ 约束与非目标**：不把逐笔裁决搬出进程；不与 compliance/alert 的既有实现重复造；不改 `RiskManagerConfig` 的默认值。
- **⑤ 验收**：锁定 hash 的规则快照可复现；换规则 → `run_spec_hash` 变；双跑对拍一致。
- **⑥ 边界**：只做规则与裁决面；聚合监控/报送属同单的后续切片。


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
| X3a | 规则定义 + revision + 审批状态机（draft→approved→active→deprecated） | `QT-42` | 本仓立项 |
| X3b | 快照内容寻址 + `resolve` / `bundle` | `QT-42` | X3a |
| X3c | 事件回传（异步批量）+ 监控聚合（敞口 / 告警） | `QT-42` | X3b |
| X3d | 熔断指令（幂等键） | `QT-42` | X3c |

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

---
status: active
last-verified: 2026-11-07
verified-by: QT-80 施工 —— risk_events.actor 留痕（claims 取值 + 两条用例 + 破坏验证），PG 集成腿未执行记 🔶；此前 SVC-RISK-G0 完成（Dockerfile + deploy/compose + 默认 8089 与 TCP 探针）；QT-74 前两步已落，迁 contracts / core 消费未做
---

# TASKS — tellurion-risk 未完成项

> 本仓**唯一**任务台账。跨仓任务只在 §二 留指针，**不镜像正文**（双写必然漂移）。
> 状态：`⬜ 待做` / `🔶 进行中` / `⛔ 阻塞（有未解除前置）` / `✅ 完成（附日期）`

> **第三轮裁决指针（2026-10-25）**：② 快照类型迁 contracts = 本仓把 `Snapshot` / `SnapshotRule` / `Scope` / `RuleType` / hash 纯函数 / 4 哨兵**改为 `contracts/risk` 的别名/转发**（本仓首次引 contracts），`Bundle` / `ParseBundle` / `ForScope` 留本仓；**编码逐字节不许改**（改 Canonical = 历史 `risk_rules_hash` 全废）。⑬ kit `v0.1.4` 发布后本仓升 pin。正文见 `tellurion:docs/design/qt-decoupling/cross-repo-decision-record-2026-10-25.md` §五。
> 每张单六字段（① 目标 ② 上下文 ③ 要求 ④ 约束与非目标 ⑤ 验收 ⑥ 边界），判据见 `tellurion:AGENTS.md` §8.5。
> 完成即标 ✅（附日期）并移入 `docs/archive/TASKS-YYYY-MM-DD-archived.md`；本文件只留未完成项。
>
> **U7 裁决（2026-10-24）**：compliance / alert 归属定为「拆分」——6 个检测器留 core 进程内 `alertLoop`，告警的**通道编排与报送**归本仓。本文所有「U7 未裁 / 本切片不做」的旧记录以此条为准；监控 3 端点（`monitor/exposure` / `alerts` / `evaluate`）**已于 2026-10-24 落地**（见下方施工记录；ADR-0004）。

---

## 一、本仓任务

> **约定变更（2026-10-21）**：原「占位仓不写正式六字段任务单」作废——X 线任务单已展开为本仓正式任务（见下）。M5/M8 开工前置不变；实现仍未开工，开工前仍须过前置两条。

### SVC-RISK-G0 · 服务就绪门 G0：自有部署与本地启动（ADR-036）[P0] ✅ 完成（2026-10-26）

- **① 目标**：本仓作为风控**控制面**服务，能独立构建、启动、被健康检查；同步裁决仍留 core 进程内。
- **② 上下文**：ADR-036 定本仓为服务，并保留「控制面服务 + core `RiskPolicy`」混合形态；今天有 `cmd/riskservice` + `risk.*` schema + kit `v0.1.4` 鉴权，但无 `Dockerfile` / `deploy/`，默认 `:8086` 与 ai-service 撞号。
- **③ 要求**：新增 `Dockerfile`（多阶段、非 root、`curl`）；新增 `deploy/compose.yaml`（`RISK_DATABASE_URL` / `JWT_SECRET`；端口 `:8089`）；**默认监听改 `127.0.0.1:8089`**（代码 + 文档 + ARCHITECTURE）；补 `config/` 或 env 说明；README 写单仓启动命令；健康检查用 TCP 探针（本仓暂无公开 `/healthz`，归 G1）。
- **④ 约束与非目标**：不改规则裁决逻辑；不接管 core 四条 `/api/risk/*` 进程内端点；不接 core `bundle→RiskPolicy`（G3）；不引入 K8s。
- **⑤ 验收**：`docker compose -f deploy/compose.yaml config` 通过；单仓启动后 TCP `8089` 可连；`:8086` 不再被本仓占用；无 `replace` / `go.work`。
- **⑥ 边界**：只改本仓 deployment 与默认端口；core 侧消费由 `SVC-RISK-G3` 跟踪。

> **施工记录（2026-10-26 · SVC-RISK-G0 落账）**
>
> 已交付（本仓）：
>
> - `Dockerfile`（新增）：多阶段（`golang:1.25-alpine` 构建 → `alpine:3.19` 运行）、非 root（`USER nobody`）、
>   `EXPOSE 8089`、**TCP 健康检查** `nc -z 127.0.0.1 8089`（`netcat-openbsd` 随镜像安装）。
>   没有造匿名 `/healthz`：公开健康端点属 G1，为本单加一个可匿名读的端点等于绕过鉴权闸。
> - `deploy/compose.yaml`（新增）：`-addr 0.0.0.0:8089`（容器内监听）、宿主发布 `127.0.0.1:8089`
>   （仍只在回环可达）；`RISK_DATABASE_URL` / `JWT_SECRET` 走 env 且有缺省；`host.docker.internal`
>   映射到 `host-gateway`，便于连宿主 PG；healthcheck 与 Dockerfile 同一 TCP 探针。
> - `cmd/riskservice/main.go`：默认监听 `127.0.0.1:8089`，抽成 `const defaultAddr` 以便被测试钉住
>   （原先写在 `flag.String` 字面量里，改回去没有任何东西会红）。
> - `cmd/riskservice/main_test.go`（新增）：`TestDefaultAddr` 断言默认值 = `127.0.0.1:8089`
>   （8086 是 ai-service 的口径，撞号会直接表现为本地联调起不来）。
> - `README.md`：默认端口订正为 8089，补 `docker build` / `docker compose -f deploy/compose.yaml up --build`
>   两条单仓启动命令 + 三个 env 的说明表 + 「健康检查是 TCP 探针」一句。
> - `docs/ARCHITECTURE.md` §3.1：`cmd/riskservice` 端口说明 + 「部署（G0）」段（唯一对外端口 8089、
>   TCP 探针、配置只经 env）。
>
> 验收（真实输出，2026-10-26 本机 Go 1.25.14）：
>
> - `docker compose -f deploy/compose.yaml config -q` → 退出 **0**。
> - `go build ./...` → 退出 **0**；`go vet ./...` → 退出 **0**；`go test ./... -count=1` →
>   `ok tellurion-risk/internal/{breaker,event,httpapi,pgstore,rule}`，退出 **0**；`gofmt -l .` 空。
> - **破坏验证**：把 `defaultAddr` 改回 `127.0.0.1:8086` ⇒ `TestDefaultAddr` 红
>   （`default listen address = "127.0.0.1:8086", want 127.0.0.1:8089`）；逐字节还原后复跑绿。
>
> 未落项（如实登记，均不在本单）：
>
> 1. ~~**Docker 镜像构建未实跑**：本机 Docker daemon 不可达（`//./pipe/dockerDesktopLinuxEngine`
>    不存在）……~~ → **已解除（2026-10-26 复测）**：Docker Engine **29.8.0** 可达；镜像已构建
>    成功（`tellurion-risk:dev`），六服务聚合栈里 `risk` 容器 **healthy**（同批唯一不健康的是
>    analytics 的 `/healthz`，已单独立单修复）。仍未做：core → risk 的真实调用链端到端腿
>    （risk 侧两条出厂默认关，见 §四 F2）。
> 2. **公开健康 / 就绪端点（`/healthz`、`/readyz`）**：本仓今天没有，探针是 TCP；G1 补。
> 3. **`config/` 目录**：本单按 ADR-036「`Dockerfile + compose 片段 + 仓内 config`」的等价口径
>    用 **compose + env 说明**兑现（本仓零配置文件，硬造一个 `config/` 只会多一份无人读的真相）。
> 4. **K8s 清单**：本单明确不做（G0 边界）。core 侧 `deploy/k8s/risk-deployment.yaml` 在 core 仓，
>    本仓不认领。

### SVC-RISK-G3 · 规则源切换与 core 本地规则退役（ADR-036）[P0] ⬜

- **① 目标**：core 的 `RiskPolicy` 真正消费本仓 `bundle`；本地 `risk_manager.*` 规则源退役，只保留同步裁决代码。
- **② 上下文**：`risk_rules_hash` 已生产，但 `bundle → ParseBundle → RiskPolicy` 零调用点；事件回传开关默认关；core `pkg/risk` 仍持本地规则配置。
- **③ 要求**：先落 QT-74 参数词典；core 在 run 前按 hash 拉 `bundle` 并构造 `RiskPolicy`；不可用时 fail-closed；历史 run 新旧裁决对拍；切换默认后删本地规则配置与旧解析；事件回传开关打开并验收。
- **④ 约束与非目标**：不把同步裁决搬到 HTTP；不改 `Canonical` 编码（历史 hash 不许废）；不做资金/下单。
- **⑤ 验收**：core `grep` 本地风险规则源 = 0；同批 run 新旧裁决一致（对拍报告归档）；`bundle` 不可用 → 拒绝运行；事件回传有真实行。
- **⑥ 边界**：本仓只保证 `bundle` 可解析、可审计；core 裁决代码删除归 **`SVC-CORE-RISK-RETIRE`** 跟踪——条目**已在 core 立单**（2026-10-26 同日补，`tellurion:docs/TASKS.md:139`「六之二、退役执行单」，串行第 7；判据逐字抄 B 组材料 §4.4）。⚠️ 本行此前写「由该 ID 跟踪」时它只有名字没有条目，那一版账已被 core 的 §六之二 推翻；开工时按彼处六字段与**当场重跑的基线数**走（基线会漂移，不许照抄文档里的数）。

### QT-42 · X3 风控服务（规则真相源 + 进程内 RiskPolicy）[P2] 🔶

> **迁入说明（2026-10-21）**：本单自 core 仓 `tellurion:docs/TASKS.md`「五、X 线」迁入（任务台账批量重分布：X 线单展开到各服务仓为正式六字段单，「占位仓不写正式任务单」旧约定同时作废）。六字段正文逐字保留、不改写；core 侧同号条目已改为指针。
>
> **M8 前置完成（2026-10-22）**：接口对表见 [`ARCHITECTURE.md`](ARCHITECTURE.md) §9。开工前先读 §9.1 的五条前置事实——`CheckOrder` **零实现零调用**、`risk_rules_hash` 写入路径已由 QT-63 落库但**恒为空**（等 X3b 填）、`/api/risk/*` 前缀已被 core 四条进程内端点占用、U7 未裁（监控半边形状不冻结）。对表当时只是开工前置、不是实现（2026-10-23 起 X3a 已实装，见下）。
> 对表自检（2026-10-22）：21 条 core `path:line` 按 HEAD `dbe4c68` 逐条命中；C 组 12 端点 / 13 错误码与 `api-schema.md` 双向一致；破坏验证三腿（改错行号 / 删一行端点 / 删一个错误码）各自红，还原后绿。
>
> **首批实装（2026-10-23）**：X3a 规则侧已落（见下方施工记录），本单状态改 🔶；X3b 亦已落（见 X3b 施工记录）；X3c / X3d 与监控半边仍未开工。

- **① 目标**：风控规则可版本化、可审计；回测与实盘调用**同一** RiskPolicy；结果能记 `risk_rules_hash`。
- **② 上下文**：`risk-service-design.md`（D20）；`risk.rule_defs`/`rule_revisions`/`rule_snapshots` 待建；`risk_events` 表未落地（`OrderIntent` 注释已指向）。
- **③ 要求**：① 规则表 + `version_hash`（复用 factor 样板）+ 快照下发 `/api/risk/snapshots/{hash}`；② 进程内 `RiskPolicy` 接口化（现状 `RiskManager` 已无状态，可直接适配）；③ `CheckOrder` **永不走 HTTP**；④ 熔断指令走异步；⑤ 结果记 `risk_rules_hash`（QT-03 分量）。
- **④ 约束与非目标**：不把逐笔裁决搬出进程；不与 compliance/alert 的既有实现重复造；不改 `RiskManagerConfig` 的默认值。
- **⑤ 验收**：锁定 hash 的规则快照可复现；换规则 → `run_spec_hash` 变；双跑对拍一致。
- **⑥ 边界**：只做规则与裁决面；聚合监控/报送属同单的后续切片。

> **施工记录（2026-10-23 · X3a 规则侧首批实装）**
>
> 已交付（本仓 `tellurion-risk`，单仓零跨仓依赖，contracts / kit 均未引入）：
>
> - `internal/rule`：冻结枚举（4 类型 / 4 作用域 / 4 状态）、`Revision.Validate`（JSON 对象 + 拒尾随内容 + 64 KiB 上限）、
>   内容寻址 `version_hash`（域 `tellurion.risk_rule_version_hash.v1`，**不含** description / status / 时间）、
>   `Store` 端口 + `Memory` 实现（全部进出深拷贝）、7 条幂等 DDL（`rule_defs` / `rule_revisions` / `audit_logs` + 2 索引）、
>   `Service`（create 幂等 / update 基准守卫 / approve 一次走完 draft→approved→active 并让旧 active 转 deprecated / 全写审计）。
> - `internal/httpapi`：4 端点（`POST /rules`、`PUT /rules/{id}`、`POST /rules/{id}/approve`、`GET /rules?scope=&active=`），
>   严格解码（`DisallowUnknownFields` + `MaxBytesReader` + 拒多对象）、13 错误码中可达的 8 个、201/200 重放语义。
> - `cmd/riskservice`：`127.0.0.1:8089` 起服务（`Memory` Store；ADR-036 起默认端口从 8086 改为 8089，避开 ai-service）。
> - 测试：`internal/rule`（hash 内容寻址 / fail-closed / 状态机 / 审计顺序 / 深拷贝 / DDL 形状）、
>   `internal/httpapi`（生命周期 / wire code 16 case / 空列表非 null / 405）。
> - 裁决落 `docs/adr/0002-规则版本-hash-与审批状态机的裁决.md`；`docs/db-schema.md` §5、`docs/api-schema.md` 实现状态段、
>   `docs/dataflow.md` 规则侧时序同步更新。
>
> 未落项（本单仍未完成，**不得**按已交付理解）：
>
> 1. **PG Store 与迁移执行入口**：`rule.Migrations()` 已交副本，但没有 PG 实现、没有启动时执行接线（当前只有 `Memory`）。
> 2. **PG 事务边界**：Service 现用进程内互斥锁串行「读-改-写」，接 PG 后必须换事务，否则并发 approve 会丢更新。
> 3. **X3b 快照**：`rule_snapshots` 表、`/snapshots/{hash}`、`POST /resolve`、`/bundle`、进程内 `RiskPolicy` 构建与 `risk_rules_hash` 回填。
> 4. **X3c 事件**：`risk.risk_events` 表与 `POST /events`（异步批量、207 partial_failure）。
> 5. **X3d 熔断**：`risk.circuit_orders` 表与 `POST /breakers`（幂等键）。
> 6. **监控 3 端点**：`monitor/exposure` / `alerts` / `evaluate`——U7（compliance / alert 归属）未裁，形状不冻结，**本切片不做**。
> 7. **鉴权与 actor**：QT-50 ④ 未落，审计 `actor` 恒为空串；`Actor` 字段已用 `json:"-"` 防止调用方自报身份，HTTP 传 `actor` 会被严格解码拒收。
> 8. **`risk.limits` 表**：限额表未建（X3b 之后）。
> 9. **审计读取无 HTTP 端点**：`Store.ListAudit` 只有进程内接口，对外查询未定（避免过早冻结形状）。
> 10. **`rule_deprecated` 错误码不可达**：冻结接口面无 deprecate 端点，实现里不写不可达分支（见 `docs/api-schema.md` §2 注）。

> **施工记录（2026-10-23 · X3b 快照侧）**
>
> 已交付（本仓，单仓零跨仓依赖不变）：
>
> - `internal/rule/snapshot.go`：`Snapshot` / `SnapshotRule` wire 形状；`SnapshotHash`
>   （域 `tellurion.risk_snapshot_hash.v1`，按 rule_id 排序 + canonical params + 长度前缀域，
>   **不含** `created_at`——同一内容永远同一 hash，缓存按 hash 永不失效；未知 type/scope fail-closed 拒收）；
>   `ParseBundle`（bytes → `Bundle` 纯函数：DisallowUnknownFields 严格解码 + 重新 derive hash 比对，
>   不符即 `ErrSnapshotMismatch` fail-closed 拒开跑）+ `Bundle.ForScope`（找不到 scope = 没有规则，不给默认限额）。
>   **这是进程内 RiskPolicy 的输入边界**：零 I/O 零 HTTP，core 侧构建 `RiskPolicy` 消费它。
> - `internal/rule`：`rule_snapshots` DDL（PK hash + 64-hex CHECK + 非空数组 CHECK，`migrationStatements` 共 8 条）；
>   `Store` 端口 + `Memory` 增加 `PutSnapshot` / `GetSnapshot`（**双头 derive 校验**：写入与读取都重算比对；
>   同 hash 重复写 = no-op，同 hash 不同内容 = fail-loud，不许静默覆盖）；哨兵 `ErrSnapshotNotFound` /
>   `ErrNoApprovedRules` / `ErrSnapshotMismatch`；`Service.Resolve`（hash / latest 二选一，latest 把当前
>   **active revisions** 收进快照并按内容寻址幂等落库；draft 永不进快照；无生效规则 → `ErrNoApprovedRules`）。
> - `internal/httpapi`：+3 端点（`POST /resolve`、`GET /snapshots/{hash}`、`GET /snapshots/{hash}/bundle`，
>   bundle 与快照本体同一 canonical JSON）+ wire code 映射（404 `snapshot_not_found` / 404 `no_approved_rules`）。
> - 测试：hash 内容寻址（排序无关 / params 书写无关 / 逐字段变更换 hash）、fail-closed（重复 rule_id /
>   未知类型 / 空快照）、resolve 只有 active 进快照（draft head 不进）、无审批拒开跑、钉 hash 不回退 latest
>   （历史快照仍可复现）、写/读双侧篡改检测、`ParseBundle` 回环 + scope 查找 + 篡改拒收；
>   wire 级同一 hash 两次拉取**逐字节一致**（dataflow 不变量 1/2/3 全部有测试名）。
> - 文档同笔：`db-schema.md` §1/§1约束/§5、`api-schema.md` 头部/§1/§2、`dataflow.md` 主链路/run 前时序/失败表/不变量、
>   `ARCHITECTURE.md` §3.1/§8/§9.3/§9.4/§9.5、`AGENTS.md` §1。
>
> 验收口径对照（本单 ⑤）：「锁定 hash 的规则快照可复现」= ✅（`TestResolve_PinnedHashNeverFallsBack` +
> wire 字节一致）；「换规则 → hash 变」= ✅（快照级内容寻址测试）；「双跑对拍一致」= ⛔ core 侧接线未做。
>
> 未落项（在原 10 条之上更新；原 3 条改写，其余不变）：
>
> 3'. **core 侧消费接线（原「X3b 快照」剩余半边）**：~~`risk_rules_hash` 生产者仍写空串（F3，QT-63 落的表）~~
>   → **2026-10-25 拆两半读**：✅ hash 半边已落（core QT-66：run 提交时调 `POST /api/risk/resolve`，
>   hash 写进 `quant.run_provenance`，空串只剩 `not_configured`）；⬜ 「拿 bundle 进 `ParseBundle`
>   构建 `RiskPolicy`」仍未接（core 全仓 `ParseBundle` 零调用点，限额来自它自己的 `risk_manager.*` 配置）。
>   这是跨仓单（core 侧改动），不在本仓。`ParseBundle` 的 Go 类型出现第二个使用方（core）后，
>   `Snapshot` / `SnapshotRule` 按 ADR-0001 ① 迁 contracts。
>   **2026-10-25 H3 前提核对（三条实测阻塞；本条仍不得整条划掉）**：派发到 core 的 H3 把「bundle → `ParseBundle`
>   → 构建 `RiskPolicy`」当成可直接开工的单，按源码核对**前提不成立**：
>   ① **规则参数词典不存在（根本阻塞）** ⚠️ **【该前提已于 2026-10-26 失效，见下方「✅ 已解除」；本段的取证行也已过期，按历史读】**：`SnapshotRule.Params` 在本仓是 opaque JSONB，四类规则的字段名 / 单位
>     **没有任何成文口径**——本仓自证夹具的形状~~就是 `{"pct":2.5}` / `{"max_pct":10}`
>     （`internal/rule/snapshot_test.go:143-144`）~~（**2026-10-26 复测：那两行今天已是 `stopLossParams` / `sizingParams` 两个常量，`git grep -n '"max_pct"' -- internal/rule` = 0 命中**），core 侧的限额面是 14 个标量
>     （`tellurion:pkg/risk/manager.go:13-28`，来自 `tellurion:pkg/bootstrap/engine.go:38-51` 的 `risk_manager.*`），
>     两边对不上表 ⇒ core 只能猜字段名与单位（`max_pct = 10` 是 10% 还是 10 倍？），而 H3 自己的禁区写着
>     「不许在 core 侧自造一套 risk 规则解释」。**源头不冻结，这条链无法开工。**
>     `risk.limits` 也补不上洞：[`db-schema.md`](db-schema.md) §1 明写「本切片只建表，读写路径与端点未冻结」
>     （本文件 5 条施工记录逐条重复此条）。⇒ 词典展开成 **`QT-74`**（本文件「一、本仓任务」末），排在 contracts 发版与
>     core 消费**之前**（顺序不可颠倒：先有词典才有值得迁的形状）。
>     **✅ 已解除（2026-10-26，QT-74 前两步）**：词典已落（`internal/rule/params.go` 的 14 标量 +
>     `db-schema.md` §7 + `api-schema.md` §1.1），写侧严格、读侧拒未知键；本条「词典不存在」的阻塞不再成立。
>     `risk.limits` 同批定性为「遗留空表、无生产者」。
>   ② **H3 自述的串行前置未落**：工单写「与 H1 的 QT-71 同仓串行」，实测 core 侧 QT-71 ①~③ **未开工**
>     （`tellurion:pkg/auth` grep `RoleService` = **0 命中**、`tellurion:cmd/analysis/route_policy.go` 无 service 档位、
>     `tellurion:docs/TASKS.md:1564` 状态仍 ⬜）⇒ H3 排在了自己的前置之前。
>   ③ **生产 `service` token 到不了角色判定**：即上面第 4 条已报协调者的缺口，源码复核成立——
>     `tellurion-kit:auth/httpadapter/httpadapter.go:139-141` 对非数字 subject 直接 401「malformed token subject」，
>     而 `tellurion-kit:auth/service_token.go:46` 把 `sub=service:<repo>` 原样签进 `Subject`（kit 自己的
>     `service_token_test.go:16/24` 用的就是 `"service:risk"`）。**这条跨仓缝无人覆盖**：kit 的 httpadapter 测试里
>     `IssueServiceToken` grep 零命中，本仓 `TestPolicy_*` 走的是 `IssueTokens(1, …)` 的数字 subject
>     （`internal/httpapi/auth_test.go:55`）⇒ 夹具绿不等于生产链路绿，与上面第 4 条同一结论。
>   **同批订正本条一处取包口径（利好）**：工单写「core 调 `POST /api/risk/resolve` 取 bundle」——实测 `resolve`
>   的响应**已整包内嵌快照**（`internal/httpapi/api.go:127-130` `resolveBody.Snapshot rule.Snapshot`），而 `resolve`
>   已开 `service` ⇒ core 取包**不需要**新开 `bundle` 档位，上面未落项第 5 条的「若 core 直接拉 `bundle` 需新开一单」
>   这条**条件不触发**，档位保持不开（本仓因此仍是零代码改动）。
>   验收三条按实测拆读：「换规则 → `run_spec_hash` 变」✅（`tellurion:pkg/backtest/qt66_risk_rules_test.go:113` 已断言）、
>   「同 hash 两次逐位一致」+「篡改即 `ErrSnapshotMismatch` fail-closed」✅（本仓 `TestAPI_ResolveLatestAndSnapshotByteIdentity`
>   + `internal/rule/snapshot_test.go:266` 篡改腿）——**唯一真未落 = 「限额来自快照」，被 ① 卡住。**

> **施工记录（2026-10-24 · PG Store + 迁移执行 + 事务边界）**
>
> 已交付（本仓，单仓可自证）：
>
> - `internal/rule`：`Store.Tx(ctx, fn)` 端口；`Service` 的 `Create` / `Update` / `Approve` / `Resolve(latest)`
>   全部在 `Tx` 内完成「读-改-写」，**删除进程内互斥锁**（ADR-0003）；`Memory.Tx` 用全量状态深拷贝 +
>   失败还原模拟回滚；`Resolve(latest)` 落库后回读快照，同 hash 的 `created_at` 不漂移。
> - `internal/rule/ddl.go`：`risk.limits` 同批建表（scope + subject + limit_key；仅建表，读写路径未冻结）。
> - `internal/pgstore`：`rule.Store` 全量 PG 实现（`Tx` = BEGIN/COMMIT/ROLLBACK；事务内 `GetDefinition`
>   用 `SELECT ... FOR UPDATE`；JSONB / TIMESTAMPTZ / NULL 映射；唯一约束冲突翻译为领域哨兵）；
>   `Migrate` 按序执行 `rule.Migrations()`。
> - `cmd/riskservice`：`-dsn` / `RISK_DATABASE_URL` 非空 → Ping → Migrate → PG Store；
>   空 DSN 显式告警后退回 Memory（仅本地开发）。
> - 依赖登记：`github.com/jackc/pgx/v5 v5.7.5`（AGENTS §4；仅进程入口空白导入 `stdlib`）。
> - 测试：Memory Tx 全量回滚、Approve 中途写失败整笔回滚、PG 唯一约束错误映射、`risk.limits` DDL 形状。
> - 文档同笔：`db-schema.md` §1/§4/§5/§6、`api-schema.md` 头部、`dataflow.md` 现状/失败表/不变量、
>   `ARCHITECTURE.md` §3.1/§5/§6/§8/§9.5、`AGENTS.md` §1/§4、ADR-0003 + ADR 索引。
>
> 验收口径：`go build ./... && go vet ./... && go test ./... -count=1` 全绿；破坏验证（把 Approve 的
> `Store.Tx` 改回直接调用 Store）→ `TestServiceApprove_RollsBackAllWritesOnFailure` 红，逐字节还原后绿。
>
> 未落项（本条**取代** X3a 记录里的第 1、2 条；其余继续有效，另加一条）：
>
> 1. ~~PG Store 与迁移执行入口~~ ✅ 2026-10-24。
> 2. ~~PG 事务边界~~ ✅ 2026-10-24（ADR-0003）。
> 3. X3c 事件：`risk.risk_events` 表与 `POST /events`（异步批量、207 partial_failure）。
> 4. X3d 熔断：`risk.circuit_orders` 表与 `POST /breakers`（幂等键）。
> 5. 监控 3 端点：`monitor/exposure` / `alerts` / `evaluate`——U7 未裁，形状不冻结，本切片不做。
> 6. 鉴权与 actor：QT-50 ④ 未落，审计 `actor` 恒为空串。
> 7. `risk.limits` 的读写路径与端点：表已建，读取口径（快照附带 / 独立端点）未冻结。
> 8. 审计读取无 HTTP 端点：`Store.ListAudit` 只有进程内接口。
> 9. `rule_deprecated` 错误码不可达（冻结接口面无 deprecate 端点）。

> 10. **core 侧消费接线（跨仓）**：~~`risk_rules_hash` 生产者仍写空串（F3）~~（2026-10-25 已落真值，见本条末）；需 core 在 run 前
>    `resolve` → `ParseBundle` → 写 `run_provenance`。不在本仓施工，已报协调者。
>    **2026-10-25 裁决已落（core `裁-10` / `裁-11`）**：core 每次 run 提交时调 `POST /api/risk/resolve`（run_spec 钉 hash 走锁定、否则 `latest`）；只有「本部署未接入 risk」允许空串并记 `not_configured`，配置了但取不到（含 `no_approved_rules` / `snapshot_not_found` / 5xx / 超时）一律 run `failed` + `risk_rules_unavailable`。本仓端点无需改动，F3 可直接开工。
>    **✅ 开工并已落（2026-10-25，core QT-66 腿）**：`risk_rules_hash` 的生产者不再写空串 —— 账在 `tellurion:docs/TASKS.md` QT-66 与 `tellurion:cmd/analysis/wiring/risk_rules.go`；本仓端点确实零改动。**剩余半边**（bundle → `ParseBundle` → 构建 `RiskPolicy`）仍未接，本条不得整条划掉。

> **施工记录（2026-10-25 · W2 鉴权接线：kit 适配 + 审计读 + 启动闸）**
>
> 已交付（本仓，ADR-0005）：
>
> - `internal/httpapi/auth.go`：`Authorizer` 接口改为返回 `Principal`；`NewAuthorizer` 接 kit
>   `v0.1.1` 的 `auth/httpadapter`（`Authenticate` = Bearer → `Parse` → 拒 `kind!=access` → 校验 subject；
>   `Authorize` = Public / AnyAuthenticated / Roles，角色 any-of）；`requireAuth` 三态不变
>   （未装配 503 / 无凭证 401 / 越权 403），通过后把身份放进 context。
> - `internal/httpapi/policy.go`：全端点「路由 → 角色」表（读 viewer、写 trader、规则发布/审批 admin；
>   `resolve` / `events` / `breakers` 按写口径，`monitor/evaluate` 按读口径）。路由注册与策略表同源，
>   `TestRouteTableEqualsPolicyTable` 双向对拍。
> - `internal/httpapi/api.go`：所有路由统一包 `requireAuth`（不再只有 monitor/*）；新增
>   `GET /api/risk/audit?rule_id=`（viewer，空列表 `[]`）；`create`/`update`/`approve` 的 `Actor`
>   取自 claims（缺失退 subject），open-access 记常量 `open-access`，**审计 actor 非空串**。
> - `cmd/riskservice`：`-jwt-secret` / `JWT_SECRET` + `-insecure-exposure` / `AUTH_INSECURE_EXPOSURE`；
>   启动闸 `DecideStartup`——有 secret 开鉴权，无 secret + 回环 = 开放，无 secret + 非回环 = **拒绝启动**。
> - 依赖登记：`github.com/ruoxizhnya/tellurion-kit v0.1.1`（AGENTS §4）。
> - 测试：`TestRouteTableEqualsPolicyTable`（路由 ≡ 策略）、`TestAuth_ThreeStatesAndActor`
>   （503/401/403 + 角色档位 + 审计 actor 来自 claims）、`TestAuth_OpenAccessOnLoopback`
>   （显式开放的 actor 仍非空）；monitor 旧用例同步到新 `Authorizer` 签名。
> - 文档同笔：`api-schema.md`（审计读 + 全端点鉴权 + actor 来源）、`db-schema.md` §6、`dataflow.md`
>   （鉴权闸步骤/失败表/不变量）、`ARCHITECTURE.md` §3.1/§9.4/§9.7 R4、`AGENTS.md` §1/§4、README、doc.go、
>   ADR-0005 + ADR 索引。
>
> 验收：`go build ./... && go vet ./... && go test ./... -count=1` 全绿。
> 破坏验证：删掉策略表里的 `GET /api/risk/audit` 条目 ⇒ `TestRouteTableEqualsPolicyTable` 红
> （route 13 vs policy 12）且 `TestAuth_ThreeStatesAndActor` 的 audit 403；逐字节还原
> （`sha256 98272F27…`）后两条复跑绿。
>
> 未落项（本条**取代**上一条列表）：
>
> 1. **外部推送通道**（webhook / 消息平台）与节流：本切片只做 HTTP 报表，不主动推送。
> 2. **core `alertLoop` 投递告警**：接线前 `alerts` / `evaluate` 只会看到空集。
> 3. **敞口口径的生产侧约定**：`account` / `strategy` / `side` / `qty` / `price` / `notional` 需生产者写入
>    `payload`；漏写按 `unpriced_events` / `unsigned_events` 显性计数。
> 4. **熔断执行方与 ACK**：`circuit_orders.status` 今天只写 `applied`；随升格 ADR。
> 5. **core 侧消费接线（跨仓）**：F3 **订正**（2026-10-25）—— ✅ `risk_rules_hash` 生产者已落真值（core QT-66，账在 `tellurion:docs/TASKS.md`）；⬜ `bundle` → `ParseBundle` → 构建 `RiskPolicy` 半边仍待（core 零调用点）。
> 6. `risk.limits` 的读写路径与端点：表已建，读取口径未冻结。
> 7. `rule_deprecated` 错误码不可达（冻结接口面无 deprecate 端点）。
> 8. **core 调本仓的 token 分发（跨仓）**：鉴权开启后 core 侧调用 `resolve` / `events` / `breakers`
>    必须带服务间凭证。**口径已定（裁-17 定稿，2026-10-25）**：使用 **`service` 角色**的长期 access token
>    （**不是** `trader` —— `trader` 同时可下实盘单，凭证泄露面不可接受），`sub=service:core`、TTL 30d、
>    只经 env（`RISK_SERVICE_RESOLVE_TOKEN` / `RISK_SERVICE_EVENTS_TOKEN`）注入。**本仓腿 ✅ 已落（2026-10-25，
>    派发块 G4 / ADR-0006）**：`internal/httpapi/policy.go` 的 `POST /api/risk/resolve` 与 `POST /api/risk/events`
>    两条 Roles 加 `"service"`（与 `trader` / `admin` **并列**，any-of 且 kit 不排序 ⇒ 不能互相顶替；
>    `GET /api/risk/audit` **不加**，规则管理面**不加**）；守卫测试
>    `TestPolicy_ServiceRoleScope` / `TestPolicy_ServiceRoleDeclaredExactlyTwice` /
>    `TestPolicy_ServiceRoleCoexistsWithHumanRoles` 已落。**剩余（跨仓）**：`service` token 的**签发与分发**
>    不在本仓（core QT-71 / kit KT-09），本仓只**认**这个角色，不签发、不存、不注入。
>
> **收编说明**：原列表的「`Authorizer` 实现」「审计读取无 HTTP 端点」「鉴权与 actor」三条已由本笔关闭。

> **施工记录（2026-10-24 · X3c 事件回传）**
>
> 已交付（本仓）：
>
> - `internal/event`：wire `Event`（`run_id` / `order_id` / `event_seq` / `symbol?` / `verdict?` /
>   `reason?` / `payload?` / `ts`）、`Validate` fail-closed、去重键 `(run_id, order_id, event_seq)`、
>   `Memory` Store、`Service.Ingest`（逐条校验；单条不合法记 `Failure{index, code, message}` 后继续；
>   未知字段不丢，落库原文进 `Record.Raw`）。空批 / 整批不可解析 → `ErrInvalidParams`。
> - `risk.risk_events` DDL（主键 = 去重键；`raw JSONB NOT NULL` 保留原文；`payload JSONB`；
>   `event_ts` / `received_at`；`CHECK (event_seq >= 0)`）。
> - `internal/pgstore`：`event.Store.PutEvents`（单事务逐行 `INSERT ... ON CONFLICT DO NOTHING`；
>   `accepted` / `duplicates` 计数；任何错误整批回滚）。
> - `internal/httpapi`：`POST /api/risk/events`（`WithEvents` 按需装配）：200 `{accepted, duplicates}`；
>   207 `{accepted, duplicates, failed[], code:"partial_failure"}`；400 `invalid_params`；落库失败 500。
> - `cmd/riskservice`：事件服务装配（PG 与规则共用 `*pgstore.Store`，Memory 模式用 `event.NewMemory`）。
> - 测试：去重（含同 run/seq、不同 order_id 必须接受）、重放、部分失败 + 未知字段保留、空批、
>   wire 200/207/400/405。
> - 文档同笔：`db-schema.md` §1/§1约束/§5/§6、`api-schema.md` 头部/§1/§2/§3、`dataflow.md` 主链路/
>   run 后/失败表/不变量、`ARCHITECTURE.md` §3.1/§8/§9.3/§9.4/§9.5、`AGENTS.md` §1。
>
> 验收口径：`go build ./... && go vet ./... && go test ./... -count=1` 全绿；破坏验证（`dedupeKey`
> 去掉 `order_id`）→ `TestIngest_DedupesByRunOrderSeq` 红，逐字节还原后绿。
>
> 未落项（本条**取代**上一条列表；X3c 只落事件回传半边）：
>
> 1. **X3d 熔断**：`risk.circuit_orders` 表与 `POST /breakers`（幂等键）。
> 2. **监控 3 端点**：`monitor/exposure` / `alerts` / `evaluate`——U7 未裁，形状不冻结，本切片不做。
> 3. 鉴权与 actor：QT-50 ④ 未落，审计 `actor` 恒为空串。
> 4. `risk.limits` 的读写路径与端点：表已建，读取口径未冻结。
> 5. 审计读取无 HTTP 端点：`Store.ListAudit` 只有进程内接口。
> 6. `rule_deprecated` 错误码不可达（冻结接口面无 deprecate 端点）。
> 7. **core 侧消费接线（跨仓）**：F3 **订正**（2026-10-25）—— ✅ `risk_rules_hash` 生产者已落真值（core QT-66，账在 `tellurion:docs/TASKS.md`）；⬜ `bundle` → `ParseBundle` → 构建 `RiskPolicy` 半边仍待。不在本仓施工。

> **施工记录（2026-10-24 · X3d 熔断指令）**
>
> 已交付（本仓）：
>
> - `internal/breaker`：wire `Command`（`breaker_id` / `action` / `scope` / `reason?` / `ts`）、
>   冻结动作枚举 `halt` / `cancel_all`、`Validate` fail-closed（未知 action / scope 拒收）、
>   幂等载荷身份 `SamePayload` = `action + scope + reason`（`ts` 不参与，重试可换时间）、
>   `Memory` Store、`Service.Apply`。
> - `risk.circuit_orders` DDL（`breaker_id` 主键；`action` / `scope` CHECK 与 Go 枚举同源；
>   `status` 今天只写 `applied`；`command_ts` 与 `received_at` 分开存）。
> - `internal/pgstore`：`breaker.Store.PutBreaker`（事务内 `INSERT ... ON CONFLICT DO NOTHING`；
>   冲突时读回并比对载荷身份：相同 → already_applied，不同 → `ErrIdempotencyConflict`）。
> - `internal/httpapi`：`POST /api/risk/breakers`（`WithBreakers` 按需装配）：
>   200 `{breaker_id, status:"applied"|"already_applied"}`；409 `idempotency_conflict`；
>   400 `invalid_params`（未知 action / scope / 缺字段）。
> - `cmd/riskservice`：熔断服务装配（PG 与规则/事件共用 `*pgstore.Store`，Memory 模式用 `breaker.NewMemory`）。
> - 测试：首次 applied、同键同载荷（不同 ts）already_applied、同键不同 action/reason → 409、
>   未知 action/scope/缺字段 → invalid_params、wire 200/409/400/405。
> - 文档同笔：`db-schema.md` §1/§1约束/§5/§6、`api-schema.md` 头部/§1/§2/§3/§4、`dataflow.md`
>   run 后/失败表/不变量、`ARCHITECTURE.md` §3.1/§8/§9.3/§9.4/§9.5、`AGENTS.md` §1。
>
> 验收口径：`go build ./... && go vet ./... && go test ./... -count=1` 全绿；破坏验证（`SamePayload`
> 把 `ts` 算进载荷身份）→ 幂等测试与 wire 重放测试红，逐字节还原后绿。
>
> 未落项（本条**取代**上一条列表）：
>
> 1. **监控 3 端点**：`monitor/exposure` / `alerts` / `evaluate`——U7 未裁，形状不冻结，本切片不做。
> 2. **熔断执行方与 ACK**：`circuit_orders.status` 今天只写 `applied`；谁执行、如何回报
>    `acknowledged/failed` 随升格 ADR。
> 3. 鉴权与 actor：QT-50 ④ 未落，审计 `actor` 恒为空串。
> 4. `risk.limits` 的读写路径与端点：表已建，读取口径未冻结。
> 5. 审计读取无 HTTP 端点：`Store.ListAudit` 只有进程内接口。
> 6. `rule_deprecated` 错误码不可达（冻结接口面无 deprecate 端点）。
> 7. **core 侧消费接线（跨仓）**：F3 **订正**（2026-10-25）—— ✅ `risk_rules_hash` 生产者已落真值（core QT-66，账在 `tellurion:docs/TASKS.md`）；⬜ `bundle` → `ParseBundle` → 构建 `RiskPolicy` 半边仍待。不在本仓施工。

> **施工记录（2026-10-24 · 监控 3 端点 / U7 拆分后）**
>
> 已交付（本仓，只读 `risk.risk_events`）：
>
> - `internal/event`：`Reader` / `Query` 只读端口（`Memory` 与 `pgstore` 双实现）；
>   `Service.Exposure` / `Alerts` / `Evaluate` 纯聚合与投影。时间窗默认 24h、上限 7d；
>   敞口 >5000 事件拒绝（不截断）；告警非法投影计 `invalid_events`；`evaluate` 不运行检测器、不写库，
>   响应 `detection:"outside_process"`；汇总触到 limit 即 400（不建立在截断数据上）。
> - `risk.risk_events`：新增 `risk_events_ts_idx (event_ts DESC)`（`internal/rule/ddl.go` **第 10 条**；2026-10-26 实测按数组顺序数：1 schema → 2 `rule_defs` → 3 `rule_revisions` → 4 `audit_logs` → 5 `rule_revisions_status_idx` → 6 `audit_logs_rule_idx` → 7 `rule_snapshots` → 8 `limits` → 9 `risk_events` → **10 `risk_events_ts_idx`** → 11 `circuit_orders`，全数组共 **11 条**。原文写「第 12 条」与 `ARCHITECTURE.md:47` 的「12 条」均为旧账，同批订正）。
> - `internal/pgstore`：`QueryEvents`（event_ts 窗口 + run/order/symbol/verdict 过滤 + 固定排序 +
>   limit+1 探测截断）；可选 PG 腿 `RISK_TEST_DSN`（未配置时 skip，非假绿）。
> - `internal/httpapi`：`GET /monitor/exposure`、`GET /monitor/alerts`、`POST /monitor/evaluate`；
>   `Authorizer` 闸 **fail-closed**：未装配 → 503 `auth_unavailable`，无凭证 → 401，越权 → 403；
>   闸只挂 monitor/*，其余端点行为不变。
> - 测试：聚合口径（gross/net/long/short、unpriced/unsigned、分组排序）、告警投影/排序/过滤/截断、
>   evaluate 只读与严重度过滤、窗口与上限 fail-closed、鉴权三态（503/401/403）、wire 空列表 `[]` 与 405。
> - 文档同笔：`api-schema.md`（三端点形状 + 事件 payload 投影 + 错误码）、`db-schema.md`（索引与告警投影）、
>   `dataflow.md`（监控读取时序/失败表/不变量）、`ARCHITECTURE.md` §3/§3.1/§8/§9、`AGENTS.md` §1、
>   ADR-0004 + ADR 索引；X3c 施工序行与本记录同步。
>
> 验收口径：`go build ./... && go vet ./... && go test ./... -count=1` 全绿；破坏验证
> （`requireAuth` 未装配时改成放行）→ `TestMonitor_NoAuthorizerFailsClosed` 红（200 vs 503），
> 逐字节还原（`sha256` 一致）后绿。
>
> 未落项（本条**取代** X3d 记录的列表）：
>
> 1. **`Authorizer` 实现（QT-50 ④ → 裁-15，2026-10-25 已定，可开工）**：monitor/* 在真机上恒 503（有意的 fail-closed）。口径：用 kit `v0.1.1` 的 net/http 适配（`Parse` + `kind==access` + 角色 any-of + 启动闸），本仓只留 policy 表；读端点 viewer、写端点 trader、规则发布/审批 admin；无 secret + 回环 = 开放，无 secret + 非回环 = **拒绝启动**；审计 `actor` 从 claims 填（不再恒空串）。守卫：route table ≡ policy table + 三态（503/401/403）。
> 2. **告警生产侧接线（跨仓）**：✅ 已落（2026-10-25）—— core 的 `core:pkg/alert`（6 个检测器 + 经
>    `AlertManager.Fire` 进来的对账 / 双跑差异）按 `POST /api/risk/events` 回传 `verdict:"alert"`，
>    落点 `core:cmd/analysis/wiring/risk_events.go`；去重键映射 `run_id="core:alert-loop"` /
>    `order_id=Alert.ID` / `event_seq=0`，`payload` = `Alert` 原样 JSON（`rule`/`severity`/`message`
>    正是本仓投影要的三个字段）。`alerts` / `evaluate` 因此不再是结构性空集。**两条限制如实登记**：
>    ① 装配开关 `risk_service.events_enabled` **默认关**，关着的部署里空集是设定；
>    ② 生产侧投递**已改为持久 outbox（2026-10-25 二批，core `tellurion:a53fe95`）**：告警先落
>    core 的 `quant.alert_outbox`（该仓 Migration 060），后台每 `events_relay_interval` 取 pending
>    重投，触 `events_max_attempts` 转 `failed` 终态但**保留行** —— 队列满即丢 / 重启即丢 /
>    不可达窗口即丢这三条都没了，「允许延迟、不允许丢」这句账从此两侧都兑得住。
>    仍存的限制：`alert_id` 撞键（同秒重启可造出同 id 两条告警）的那条不落新行，core 侧
>    转 Warn 点名但不保证送达；真进程端到端腿仍未跑（下面 ④-3）。
> 3. **外部推送通道**（webhook / 消息平台）与节流：本切片只做 HTTP 报表，不主动推送。
> 4. **敞口口径的生产侧约定**：`account` / `strategy` / `side` / `qty` / `price` / `notional` 需由生产者
>    写入 `payload`；漏写按 `unpriced_events` / `unsigned_events` 显性计数，不补零。
> 5. **熔断执行方与 ACK**：`circuit_orders.status` 今天只写 `applied`；随升格 ADR。
> 6. **core 侧消费接线（跨仓）**：F3 **订正**（2026-10-25）—— ✅ `risk_rules_hash` 生产者已落真值（core QT-66，账在 `tellurion:docs/TASKS.md`）；⬜ `bundle` → `ParseBundle` → 构建 `RiskPolicy` 半边仍待（core 零调用点）。
> 7. `risk.limits` 的读写路径与端点：表已建，读取口径未冻结。
> 8. 审计读取无 HTTP 端点：`Store.ListAudit` 只有进程内接口。
> 9. `rule_deprecated` 错误码不可达（冻结接口面无 deprecate 端点）。

> **施工记录（2026-10-25 · 跨仓：告警生产侧接线落地 + F3 账目订正）**
>
> 本仓**代码零改动**（票面「risk 的 `alerts` / `evaluate` 不再是空集」靠的是生产侧接线，端点与投影早就对了）。
>
> ① **生产侧接线（QT-42 X3c 的 core 腿）✅**：core 新增 `core:cmd/analysis/wiring/risk_events.go`
> （`RiskEventSink` 实现 `alert.Channel`）把告警攒批 `POST /api/risk/events`。映射与本仓契约的对照：
> 外层信封只有 `events`（本仓 `DisallowUnknownFields`）、`verdict="alert"`（`event.VerdictAlert` 同值）、
> `payload` = `Alert` 原样 JSON 且 `severity` 词表 `info/warning/critical` 与 `monitor.severityRank` 同源、
> `ts` 取告警时间（非零，过 `Event.Validate`）。去重键 `(run_id, order_id, event_seq)` 本是订单坐标，
> 告警两者都不属于 ⇒ 取 `"core:alert-loop"` / `Alert.ID` / `0`；`monitor/alerts?run_id=` 因此只有生产者含义。
> 验收：17 条 `httptest` 单测（含 wire 逐键断言、207 点名、`duplicates` 转 Warn、背压不阻塞、装配开关）；
> 破坏验证 = `verdict` 改成 `"risk_alert"` + `Authorization` 头改名 ⇒ 三条红，逐字节还原（sha256 `95f55721…` 相同）⇒ 绿。
>
> ② **F3 拆两半（票面「QT-66 已落 ⇒ `risk_rules_hash` 生产者已非空串」核对结论）**：hash 半边**已落**
> （`git merge-base --is-ancestor 0e267c6 main` 实测为真；`core:cmd/analysis/wiring/risk_rules.go` 每次 run
> 调 `POST /api/risk/resolve` 取 64 位小写 hex 写进 `quant.run_provenance`）。但**同一条 F3 里的
> 「拿 bundle 进 `ParseBundle` 构建 `RiskPolicy`」半边没落** —— core 全仓 grep `ParseBundle` 零调用点，
> 进程内限额仍来自 `config/analysis-service.yaml` 的 `risk_manager.*`。所以 F3 不能整条关闭，
> 本次把本仓六处「生产者仍写空串」的旧账逐条改成「✅ hash 半边 / ⬜ bundle 半边」。
>
> ③ **票面「actor 取自 claims」的核对结论（前提需订正）**：这条**不是**事件回传侧的待办。
> `actorOf`（`internal/httpapi/auth.go`）今天只作用于规则 create/update/approve 三处，写的是
> `risk.audit_logs.actor`；`risk.risk_events` **没有 actor 列**，`POST /events` 也不写审计行。
> 要让事件带调用方身份 = 加列 + 加审计写（DDL 变更，另立单），不在本票范围。已登记为新未落项：
>
> ④ **本仓新增未落项（本批只登记，不做）**：
> 1. `POST /events` 不落审计行 / `risk_events` 无 actor 列（见 ③）。
> 2. ~~生产侧投递不满足「不允许丢」：队列满即丢弃、5xx / 207 不重发、无 outbox~~
>    **已闭合（2026-10-25 二批，core `tellurion:a53fe95`）**：core 侧新增 `quant.alert_outbox`
>    + 后台 relay 重投（见上文限制 ② 的订正）。本仓不再有这条欠账；剩下的只有
>    撞键那一条没有新行（core 侧 Warn 点名）与下面第 3 条的端到端腿。
> 3. 真实投递腿只在 core 侧用 `httptest` 自证；「core 真进程 → risk 真进程 → `monitor/alerts` 读出非空」
>    这条端到端**仍未跑过**（需 risk 服务起来 + `events_enabled=true` + 一个 trader/admin token）。

> **施工记录（2026-10-25 · 派发块 G4 · 服务间角色 `service` 落策略表）**
>
> 已交付（本仓，ADR-0006）：
>
> - `internal/httpapi/policy.go`：`POST /api/risk/resolve` 与 `POST /api/risk/events` 两条 Roles 加
>   `"service"`。**与 `trader` / `admin` 并列写进同一条 `Roles`**——kit 的角色判定是 any-of 且
>   **不做排序**，`service` 不是「比 admin 低一档」的位置，替换掉真人角色会让真人调用方直接 403。
>   **`GET /api/risk/audit` 未加**（审计是人操作留痕，机器凭证不读）；规则管理面
>   （`rules` / `approve`）、`breakers`、`monitor/*` 一律未加；`riskPolicy` 之外**零改动**。
>   端点条数不变：route 13 ≡ policy 13。
> - `internal/httpapi/policy_test.go`（新增）：`TestPolicy_ServiceRoleScope`（service 调 resolve 200 /
>   events 200 / **audit 403** / 建规则 403 / approve 403 / **viewer 调 resolve 仍 403**——新增档位
>   不放松既有语义）、`TestPolicy_ServiceRoleDeclaredExactlyTwice`（service 只声明在这两条端点上，
>   多一处即权限面被放大）、`TestPolicy_ServiceRoleCoexistsWithHumanRoles`（service 与真人角色同在一条
>   `Roles`）。三条用**真 kit `TokenService`** 签发 `service` 角色的 access token，不打桩。
> - 文档同笔：`api-schema.md`（头部鉴权段 + 运行接入段 + §2 错误码表 + 角色矩阵）、`dataflow.md`
>   （主链路服务间凭证段 + 鉴权闸步骤 0 + run 前 / run 后时序 + 失败表新增一行 + 不变量 16）、
>   `ARCHITECTURE.md`（§6 ADR 索引补 0005/0006 + §9.4 鉴权段 + C 组两条端点 + R4 待裁行）、
>   `AGENTS.md` §1、`README.md`、ADR-0006 + ADR 索引；ADR-0005 §3 补裁一行指向 0006。
>
> 验收：`gofmt -l .` 空；`go build ./...` / `go vet ./...` / `go test ./... -count=1` 全绿。
> 破坏验证（两条，均实测真退出码）：
> ① 从 `riskPolicy` 删掉 `POST /api/risk/events` 条目 ⇒ **四条守卫同时红**（MUT1_EXIT=1）：
>    `TestRouteTableEqualsPolicyTable` 报 `route table has 13 entries, policy has 12`；
>    `TestPolicy_ServiceRoleScope` 报 `service events: 403`；
>    `TestPolicy_ServiceRoleDeclaredExactlyTwice` 报 `role "service" declared on 1 routes, want 2`；
>    `TestPolicy_ServiceRoleCoexistsWithHumanRoles` 报 `route "POST /api/risk/events" has no policy entry`。
> ② 只从 `resolve` 的 Roles 里删掉 `"service"` ⇒ 三条新用例全红（`service resolve: 403` /
>    `must declare role "service"` / `must include "service"`，MUT2_EXIT=1）。
> 两次**逐字节还原**（`sha256 65F809D2AFCB92EC69E5C2E6AD3FE05CCF7D8BDB1EAAFF2851F0B8C5738565B7`
> 与改前逐字节一致）后复跑绿（RESTORED_EXIT=0）。
>
> 未落项（本仓腿做完了，**这些一条都没在本仓落**，如实登记）：
>
> 1. **core 侧消费接线（跨仓，主体在 core）**：`bundle` → `rule.ParseBundle` → 构建进程内 `RiskPolicy`
>    **仍未接** —— 2026-10-25 实测 core 全仓 `grep ParseBundle` = **0 命中**（只有文档提及），
>    进程内限额仍来自 `config/analysis-service.yaml` 的 `risk_manager.*`。**不在本仓施工，已报协调者**
>    （core QT-71 之后）。本仓只提供 `Snapshot` / `SnapshotRule` / `Bundle` 三类型
>    （`internal/rule/snapshot.go:24/37/121`）+ `ParseBundle`（bytes → Bundle 纯函数，双头 derive 校验，
>    不符即 `ErrSnapshotMismatch` fail-closed）。
> 2. **`Snapshot` / `SnapshotRule` 迁 contracts**：台账 QT-42 ④ 明写「`ParseBundle` 的 Go 类型出现
>    第二个使用方（core）后按 ADR-0001 ① 迁 contracts」⇒ **迁 contracts 是下一张单，不是本单**。
> 3. **`service` token 的签发与分发（跨仓）**：本仓只**认**这个角色，**不签发、不存、不注入**；
>    签发入口属 kit KT-09（`v0.1.2`），分发与部署清单属 core QT-71。
> 4. **⚠️ 跨仓缺口：生产 service token 的 subject 形状与 kit 的守卫冲突（本单测出来了，必须报协调者）**：
>    裁-17 的生产口径是 `sub=service:<repo>`（kit `v0.1.2` 的 `auth.IssueServiceToken` 就是这么签的），
>    而 kit `auth/httpadapter.Authenticate` 要求 subject 可解析为 **`int64`**
>    （`auth/httpadapter/httpadapter.go:139` 的 `claims.UserIDInt64()`）。
>    **实测（2026-10-25，本机跑 kit `v0.1.2` 的真代码）**：
>    `IssueServiceToken("service:core","service",30d)` 签出的 token
>    ⇒ `claims.Subject == "service:core"`（非数字）⇒ `Authenticate` 返回
>    `httpadapter: unauthenticated: malformed token subject` ⇒ **401**；
>    对照组 `IssueTokens(1,"bob","trader")` 的数字 subject ⇒ 通过。
>    ⇒ **今天即使 core 拿到了 service token，它在本仓也会被 401 挡在门外，压根到不了角色判定。**
>    （kit 自己的台账 `tellurion-kit:docs/TASKS.md` KT-09 结案段也把这条写成「跨仓前置（未落）」，
>    口径是「core QT-71 / G4 在接入服务间调用前必须先裁决并放宽该校验（或改口径用数字 subject）」。）
>    **本单只证本仓策略表**（角色档位与作用域）——`TestPolicy_ServiceRoleScope` 用的是
>    kit `IssueTokens` 签的**数字 subject** token，所以它证明「策略表对」，
>    **不**证明「生产 token 能走通」。**本仓不能自己修**：放宽 subject 校验是 kit 的机制语义
>    （机制在 kit、策略在本仓，ADR-0005），本仓改不得也不该改 ⇒ **已报协调者**，
>    需 core QT-71 开工前先裁决（放宽 kit 校验 vs 改裁-17 的 subject 口径）。
>    **✅ 已闭合（2026-10-25，kit `KT-11` / `v0.1.4`）**：裁决 §五 ⑬ 拍定方向 3 —— kit 的两条守卫对
>    `claims.Role == auth.ServiceRole` 跳过 `int64` subject 校验（用户 token 一字未改），本仓 pin 已从
>    `v0.1.1` 升到 `v0.1.4`。**闭合的证据是 CI 不是本机**：本机无 Go 工具链，`go build/vet/test` 一条都没跑，
>    pin 的正确性由本仓 CI 核对（`GOSUMDB` 验证 go.sum + build/vet/test）。第 7 条那条真进程腿仍**未跑**。
> 5. **`GET /api/risk/snapshots/{hash}` 与 `/bundle` 未开 `service`**：本单口径只开 `resolve` / `events`
>    两条（裁-17 原文）。若 core 后续要用 `service` 直接拉 `bundle`（而不是经 `resolve` 的响应），
>    需要**新开一单**扩档位——本单**不许**顺手扩（禁区：不动 `riskPolicy` 之外的端点档位）。
> 6. **`rule_deprecated` 错误码不可达**：冻结接口面无 deprecate 端点，本单**没动**这个状态。
> 7. **真实进程端到端腿**：「core 真进程 → risk 真进程 → `service` token 调 resolve/events」这条
>    **仍未跑过**——本单只在 `httptest` 层用真 kit `TokenService` 自证（无部署清单、无 token 分发面），
>    且上面第 4 条的 subject 缺口**先**要解决，否则这条腿连 200 都到不了。

---

### QT-74 · 规则参数词典：四类规则的 `params` 字段名 / 单位 / scope 消费口径 [P1] 🔶（本批：词典 + 写侧严格 + 读侧未知键拒收已落；迁 contracts / core 消费未做）

> 2026-10-25 由 H3（core 侧消费接线）前提核对展开成六字段单。触发点是 QT-42 未落项 3' 的 ①：**源头不冻结词典，
> `bundle` → `ParseBundle` → `RiskPolicy` 这条链只能由消费方猜字段名和单位**——那是本项目算错过钱的缺陷类型
> （contracts `AGENTS.md` §4 钉的 fee 漂移事故：滑点写成 0.001、佣金写成 0.00025）。本单**只在本仓落词典**，
> 迁 contracts 与 core 消费都在本单之后（顺序不可颠倒：先有词典才有值得迁的形状）。

- **① 目标**：`docs/db-schema.md` + `docs/api-schema.md` 里出现一份**四类规则各自的 `params` 字段清单**，每字段带
  名称 / 类型 / 单位 / 取值域 / 必填性，使 core 能把 bundle **逐字段**映射到它的限额面，而无需读 risk 的实现或猜单位。
- **② 上下文（2026-10-25 实测）**：`SnapshotRule.Params` 是 `json.RawMessage`，`canonicalParams`
  （`internal/rule/model.go:150`）只校验「是非空 JSON 对象、≤64 KiB」，**不看键名**；`RuleType` 四个值冻结在
  `internal/rule/model.go:15-18`。消费侧形状 = `tellurion:pkg/risk/manager.go:13-28` 的 14 个标量，
  今天的来源是 `tellurion:config/analysis-service.yaml:155-173` 的 `risk_manager.*`（映射代码
  `tellurion:pkg/bootstrap/engine.go:38-51`）。**词典按这 14 项 1:1 提案**（键名与 yaml 同名，改名叫 core 猜）：

  | `type` | 字段 | 类型 | 单位 / 口径 | 现值 | 必填 |
  |---|---|---|---|---|---|
  | `position_sizing` | `target_volatility` | float | 年化**小数**（0.15 = 15%） | 0.15 | ✅ |
  | | `max_position_weight` | float | 组合净值**小数**占比，`(0,1]` | 0.10 | ✅ |
  | | `min_position_weight` | float | 同上，`[0, max]` | 0.01 | ✅ |
  | | `lookback_days` | int | 交易日，`>0` | 20 | ✅ |
  | | `annualization_factor` | float | `>0`（√252 ≈ 16.0） | 16.0 | ✅ |
  | `stop_loss` | `atr_period` | int | bar 数，`>0` | 14 | ✅ |
  | | `base_multiplier` / `bull_multiplier` / `bear_multiplier` / `sideways_multiplier` | float | **ATR 倍数**，`>0` | 2.5 / 2.0 / 3.5 / 2.5 | ✅ |
  | `take_profit` | `atr_multiplier` | float | ATR 倍数，`>0` | 3.0 | ✅ |
  | `regime` | `fast_ma_period` / `slow_ma_period` / `vol_lookback` | int | bar 数，`>0`；且 `fast < slow` | 10 / 20 / 60 | ✅ |

  **单位是这张表里唯一会让人算错钱的列**：权重一律**小数**，禁止 `pct` / 百分数形态——本单当时举的反例是
  夹具里的 `{"max_pct":10}`（按小数读是 10 倍净值、按百分数读是 10%，差两个数量级）。
  ⚠️ **该反例今天已不存在（2026-10-26 复测）**：夹具已按本单改成 `sizingParams`（`max_position_weight` 小数形态），
  `internal/rule/snapshot_test.go:143-144` 现指向 `stopLossParams` / `sizingParams` 两个常量，
  `git grep -n '"max_pct"' -- internal/rule` = **0 命中**（改键登记在本单施工记录）。**引用本行当证据前先跑那条 grep。**
- **③ 要求**：① 上表进 `docs/db-schema.md`（`rule_snapshots.rules[].params` 节）+ `docs/api-schema.md`
  （规则 create/update 的输入口径），两处同名同序，**词典是唯一真相源**；
  ② **写侧严格**：`Create` / `Update` 对 `params` 按 `type` 校验键集合与取值（未知键、缺必填、类型不符、
  `NaN`/`Inf`、`fast_ma_period >= slow_ma_period` 一律 `ErrInvalidParams` → 400 `invalid_params`）；
  ③ **读侧不重校键集合**：`ParseBundle` / `SnapshotHash` **不**因词典新增键而拒绝历史快照——否则给词典加一个键
  会让全部历史 `risk_rules_hash` 集体不可读（与本仓「钉住的历史 hash 永不失效」不变量 8 冲突）；
  ④ **scope 消费口径**：一期 core 只消费 `scope = global`；`account` / `strategy` / `symbol` 三档在 bundle 里
  **可见但本 run 不参与裁决**，这一条要显式写进 `docs/dataflow.md`，**不许静默忽略**（回测没有账户身份是事实，
  不是缺陷；但「下发了却没用」必须看得见）；
  ⑤ `risk.limits` 的 `limit_key`：本单**要么**冻结它并给词典的引用，**要么**在 `docs/db-schema.md` §1 明确
  「限额走 `rule_snapshots`，`risk.limits` 是遗留空表、无生产者」——**二选一，不许继续两头都不写**（这条在
  本文件 5 条施工记录里重复了 5 次）；
  ⑥ 词典的**变更规则成文**：加字段/加类型 = MINOR；**改某字段的单位 = breaking 且必须换域标签**
  （`DomainSnapshotHash` / `DomainVersionHash` 升 v2）——因为单位改了而键名没变时 **hash 一个字节都不变**，
  历史 run 的含义会静默漂移，这是本单最危险的一条。
- **④ 约束与非目标**：**不**迁 contracts（那是词典落定后、core 真要消费时的下一张单）；**不**改 `SnapshotHash` /
  `canonicalParams` 的编码算法（改 = 全历史 hash 作废，要 ADR 级裁决）；**不**碰 core 的任何文件；
  **不**给 `bundle` / `snapshots/{hash}` 开 `service` 档位（H3 核对已证 `resolve` 响应内嵌快照，条件不触发）；
  **不**在本单实现 core 的 `RiskPolicy` 装配；不改四类 `RuleType` 的既有取值集（新增另计 MINOR）。
- **⑤ 验收**：① 词典在两份文档里逐字段可查（字段名 / 类型 / 单位 / 必填四列齐，无「待定」）；
  ② 每条词典规则有 fail-closed 测试（未知键 / 缺必填 / 单位形态错 / `NaN` / `fast>=slow` 各一条，
  断言哨兵是 `ErrInvalidParams`）；③ 历史快照兼容腿：对**词典之前**写入的 params 原文调 `ParseBundle` 仍成功
  （读侧不重校）；④ `go build ./... && go vet ./... && go test ./... -count=1` 全绿（真退出码）。
  **破坏验证**：把一个必填键从词典删掉但保留夹具里的该键 ⇒ 写侧「未知键即拒」那条必须红；
  把 `max_position_weight` 夹具值从 `0.10` 改成 `10`（百分数形态）⇒ 取值域校验必须红。两处逐字节还原后复跑绿。
- **⑥ 边界**：只到「本仓把 params 的形状与单位说清楚 + 写侧守住」；`Snapshot`/`SnapshotRule`/`Bundle` 迁 contracts、
  core 侧 bundle→限额映射、`risk.limits` 若要端点则另立单，**都在本单之外**。

> **施工记录（2026-10-26 · 派发块 E 组 · QT-74 前两步）**
>
> 已交付（本仓）：
>
> - `internal/rule/params.go`（新增）：`ParamKind`（`float` / `int`）+ `ParamSpec`（字段名 / 类型 / 单位 /
>   取值域区间 / 必填）+ `paramsByType`（**14 个标量**，按四类 `RuleType` 分组：`position_sizing` 5 /
>   `stop_loss` 5 / `take_profit` 1 / `regime` 3）+ `ParamsDictionary()`（返回副本）。
>   **写侧** `validateParams`：未知键 / 缺必填 / 类型不符（int 给小数）/ `NaN` / `Inf` / 越出取值域 /
>   `fast_ma_period >= slow_ma_period` / `min_position_weight > max_position_weight` 一律 `ErrInvalidParams`
>   → 400 `invalid_params`。**读侧** `rejectUnknownParamKeys`：只拒词典外的键名。
> - `internal/rule/model.go`：`Revision.Validate` 在 canonical 化与 64 KiB 上限之后接**写侧词典校验**
>   （`Create` / `Update` 共用这一条路径，所以两条写入口同时被守住）。
> - `internal/rule/snapshot.go`：`ParseBundle` 在 hash 复算之后逐条 `rejectUnknownParamKeys`；
>   **不**重校必填与取值域（理由见下「与 ③ 的差异」）。`SnapshotHash` / `canonicalParams` 编码**一字未改**。
> - 夹具改键（两包全量）：`internal/rule/snapshot_test.go:144` 的 `{"max_pct":10}` → `sizingParams`
>   （`max_position_weight` = `0.10` 小数形态）；同批把 `{"pct":…}` / `{"tiers":…}` / `{"w":…}` / `{"window":…}`
>   全部换成词典键（`rule_test.go` / `snapshot_test.go` / `tx_test.go` / `httpapi/api_test.go` /
>   `httpapi/snapshot_test.go`）。`git grep -n '"max_pct"' -- internal/rule` = **0 命中**。
> - 测试：`TestParamsDictionary_IsPinned`（14 项逐项钉住名称/类型/单位 + `ParamsDictionary` 返回副本）、
>   `TestValidate_RejectsParamsOutsideDictionary`（9 条 fail-closed，并断言**不落库**）、
>   `TestValidate_AcceptsDictionaryShapes`（四类合法形状 + 权重上界 1）、
>   `TestParseBundle_RejectsUnknownParamKey`、`TestParseBundle_DoesNotRecheckValueDomain`。
> - 文档同笔：`db-schema.md` §7（词典唯一真相源 + 变更规则 + `risk.limits` 处置）、§1/§6 的 `risk.limits`
>   定性；`api-schema.md` §1.1（对外输入口径，与 §7 同名同序）；`dataflow.md` 规则变更步骤 1、
>   新增「scope 消费口径」节、失败表一行、不变量 17。
>
> 验收（真实输出，2026-10-26 本机 Go 1.25.14）：
>
> - `go build ./...` → **0**；`go vet ./...` → **0**；`go test ./... -count=1` →
>   `ok tellurion-risk/{cmd/riskservice,internal/breaker,internal/event,internal/httpapi,internal/pgstore,internal/rule}`，
>   退出 **0**；`gofmt -l .` 空。
> - `docker compose -f deploy/compose.yaml config -q` → **0**。
> - `git grep -n '"max_pct"' -- internal/rule` → **0 命中**（退出 1 = 无匹配；本机无 `rg`，用等价 `git grep`）。
> - **破坏验证 ①（键名）**：把夹具里的 `max_position_weight` 改回 `max_pct` ⇒ `go test ./internal/rule` **红**
>   （4 条：`unknown param "max_pct" for rule type "position_sizing"`），`MUT1_EXIT=1`。
> - **破坏验证 ②（单位形态）**：把夹具值从 `0.10` 改成 `10`（百分数形态）⇒ **红**
>   （4 条：`param "max_position_weight" (10) is outside its domain (0,1]`），`MUT2_EXIT=1`。
> - 两次**逐字节还原**（`sha256 0F4CA6C35678E858…` 与改前一致）后复跑绿（`RESTORED1_EXIT=0`）。
>
> **与 ③「读侧不重校键集合」的差异（如实登记，不悄悄改口径）**：
>
> - 本批派发原文写「`ParseBundle` 对未知键 fail-closed」，而 ③ 写「读侧不重校键集合」。实现取**派发原文**并收窄：
>   **只拒词典外的键名**，**不**校验必填、**不**校验取值域。
> - 相容性论证：词典的演进纪律是「**只增不删、不改单位口径**」（`db-schema.md` §7.3），所以历史快照的键必然仍在词典内；
>   真正会让历史 hash 不可读的是「重校必填 / 取值域」和「删键改名」，这两条**都没做**
>   （`TestParseBundle_DoesNotRecheckValueDomain` 用百分数形态的快照证明读侧不重校取值域）。
> - **风险**：词典**之前**写入的旧键快照（如 `{"pct":…}`）本批之后**读不回来**。今天无生产数据
>   （G0 之前本仓从未部署），影响为零；一旦有真实历史表，删键 / 改名必须先升域标签（v2），不许直接改词典。
>
> 未落项（本单仍未完结，**不得**按已交付理解）：
>
> 1. **迁 contracts（跨仓，A 组立单）**：`Snapshot` / `SnapshotRule` / `Scope` / `RuleType` / hash 纯函数 /
>    4 哨兵按 ADR-0001 ① 迁 `contracts/risk`，**编码逐字节搬运**。本批**未迁**（禁区）。
> 2. **core 消费侧（跨仓，`SVC-RISK-G3` / B 组）**：`bundle → ParseBundle → RiskPolicy` 与
>    `risk_manager.*` 的 14 标量逐字段映射；本批只保证「词典可查、写侧守住」。
> 3. **数值换算口径（§5 第 7 条待定）**：本仓已按 QT-74 ② 的表把权重冻结为**小数**；
>    core 侧 `risk_manager.*` 若与小数不同量纲，换算与新旧对拍仍待裁决，**不在本仓**。
> 4. **`risk.limits` 启用**：本批已二选一取「遗留空表、无生产者」（不算欠账）；将来启用需先立单 + 改 `db-schema.md` §7.4。

---

> **开工前置（refactor-plan-review M5/M8，2026-10-20 登记；2026-10-22 更新执行状态）**：本表任何单开工前，必须先过 core `tellurion:docs/TASKS.md`「五、X 线」的**公共前置**两条：
> ① ~~**M5 已结案（2026-10-21）** → `tellurion:docs/design/qt-decoupling/repo-inventory.md` §1.2：**六仓 ≠ 六进程**，本仓今天只到「仓内实现 + core 侧 port」，不升格容器~~ → **已被 ADR-036（2026-10-26）取代**：本仓定位为服务（控制面），G0 补 `Dockerfile + deploy`；同步裁决仍留 core 进程内。U7（compliance / alert 归属）**已裁（2026-10-24，见文首：检测器留 core，通道编排与报送归本仓）**。
> ② **M8 接口对表已落（2026-10-22）** → 本仓 [`ARCHITECTURE.md`](ARCHITECTURE.md) §9（模板 = `tellurion:docs/design/qt-decoupling/data-service-contract-and-ownership.md` §4.1；**本行原指的 `target-module-topology.md` UnifiedDataPoint 节是幻影引用，已订正**）。
> 正文以 core 彼处为准，此处只留指针、不镜像。

### SVC-RISK-G1 · 服务就绪门 G1：公开健康/就绪端点 · 错误模型闭合 · 版本化（ADR-036）[P0] 🔶（2026-11-07 代码与文档已落；真实 PG 与 compose 腿未跑）

- **① 目标**：本仓 13 条端点补齐 ADR-036 §3 的 G1 五件，并关掉台账里那条**已写明却无人执行的承诺**。
- **② 上下文**：台账 `:67` 原文「**公开健康 / 就绪端点（`/healthz`、`/readyz`）：本仓今天没有，探针是 TCP；G1 补**」——而此前**不存在 G1 单**（实测本文件此前只有 `SVC-RISK-G0` 与 `SVC-RISK-G3`）；实测 `internal/httpapi` 与 `cmd/riskservice` 里 `healthz` / `health` **0 命中**，`deploy/compose.yaml:20-23` 的健康检查是 `nc -z 127.0.0.1 8089`（TCP，只能证明端口在听，证明不了规则库可用）。已落的一半：鉴权完整（`policy.go` 策略表 + kit `auth/httpadapter`，全端点 fail-closed；`service` 角色只开 `POST /api/risk/resolve` 与 `POST /api/risk/events` 两条，ADR-0006）；分页在 `rules` 与 `audit` 两条读面上部分已有。**未落**：版本化兼容策略、错误码闭合表、限流、幂等键口径（`breakers` 有幂等键、`events` 有去重键，但两者的「重复请求返回什么」未成文）。
- **③ 要求**：① 公开 `/healthz`（无鉴权，进程活）+ `/readyz`（PG 可连 + `rule.Migrations()` 已执行，未就绪 503）；`deploy/compose.yaml` 的健康检查从 TCP 换成 `/healthz`，就绪探针用 `/readyz`；② 错误码闭合表：`internal/rule` 与 `internal/event` / `internal/breaker` 的每个哨兵 → HTTP 码 + 报文形状逐条列进 `api-schema.md`，未列出即视为缺陷；③ 幂等语义成文：`POST /breakers` 的幂等键重复返回 200 还是 409、`POST /events` 的去重键重复是否计入 `duplicates` 并返回什么码，各写死并各有用例；④ 分页：`rules` / `audit` / `monitor/*` 四条读面统一 `limit`+`cursor`（或显式声明整段返回 + 上界）；⑤ 版本化：`api-schema.md` 给兼容性策略行（加字段 MINOR / 改语义 MAJOR）；⑥ 限流按单人自托管取「不打挂自己」量级，且 **429 与 401/403 可区分**（core 侧 AUD-57 的同型坑：探针被限流读成未登录）。
- **④ 约束与非目标**：不扩 `service` 角色的档位（`/audit` 与规则管理面**不开**，ADR-0006 已裁）；不改裁决语义（`CheckOrder` 永不在本仓，ADR-036 §6 第 2 条）；不做多租户/RBAC；不引新依赖（kit 已 `v0.1.4`）。
- **⑤ 验收**：① `/healthz` 200 无需 token、`/readyz` 在 PG 断开时 503、恢复后 200（三条用例）；② `compose` 的 healthcheck 已换成 HTTP 探针且 `docker compose up --wait` 到 healthy；③ 错误码表逐条对得上实现（抽 3 条）；④ 幂等两条用例：同键重复 `breakers` ⇒ 预期码、同去重键重复 `events` ⇒ 预期 `duplicates`；⑤ 429 与 401 用例可区分且探针豁免；⑥ 三条命令单仓绿。
- **⑥ 边界**：本仓接口面；core 侧的 `bundle → RiskPolicy` 接线归 `tellurion:SVC-CORE-RISK-RETIRE`，`events_enabled` 出厂值与凭证分发归 `tellurion:QT-77`。

> **施工记录（2026-11-07）**：
> - **① 健康面**：`internal/httpapi` 新增公开 `GET /healthz`（只证进程活，不读库、不看鉴权配置）与 `GET /readyz`（注入的 readiness：PG 可连 + `risk.rule_defs` 已存在；不注入 = 内存档，响应带 `store:"memory"`）；两者**不鉴权、不限流**。`cmd/riskservice` 的 PG 档在 `openStores` 注入 readiness；Dockerfile 与 `deploy/compose.yaml` 的探针从 `nc -z` 换成 `wget -qO- http://127.0.0.1:8089/healthz`（Alpine busybox），并移除不再需要的 `netcat-openbsd`。
> - **② 启动边界**：连接 + 迁移共用一个超时，旋钮 `RISK_DB_TIMEOUT`（默认 15s，非法值告警后回落）；迁移仍只走 `pgstore.Migrate`，单副本执行（多副本前须加 advisory lock，已写进 api-schema §1 治理说明）。
> - **③ 错误码闭合表**：`api-schema.md` §1 新增 `code → HTTP → 领域哨兵` 全表（`classify` 是唯一实现）；`TestClassifyMatchesDocumentedTable` 抽 3 条对拍（`rule_not_found` 404 / `idempotency_conflict` 409 / `revision_conflict` 409）。
> - **④ 幂等语义写死**（均已有既有用例，本轮只成文不改实现）：`POST /rules` 同 id 同内容 200、不同内容 409；`POST /breakers` 同键同载荷 200 `applied:false`、不同载荷 409；`POST /events` 重复行计入 `duplicates`、整批仍 200。
> - **⑤ 分页**：统一策略 = **整段返回 + 声明上界**（`rules` 全量并按 scope/active 收窄；`audit` 必须按 rule_id；`monitor/*` 必须给时间窗），本轮不引 cursor；写进 api-schema §1，并写明「将来引 cursor 必须四端同批 + 升 MINOR」。
> - **⑥ 限流**：按身份（`username`→`subject`→`open-access`）令牌桶，默认 50 rps / 突发 200，无新依赖；超限 `429` + `code:"rate_limited"` + `Retry-After`；**鉴权失败先于限流**，`401/403` 不会被 429 顶替；探针豁免。
> - **验收（真实输出）**：`gofmt -l .` 空输出 / `BUILD_EXIT=0` / `VET_EXIT=0` / `TEST_EXIT=0`（6 个包全 ok）；**真实二进制 smoke**（内存档、`127.0.0.1:18089`）：`/healthz` → 200 `{"status":"ok"}`、`/readyz` → 200 `{"status":"ready"}`、`/api/risk/rules` → 200 `{"rules":[],"count":0}`；临时 exe 已清理。
> - **破坏验证（三条，均红后逐行还原复绿）**：① 关掉限流判定 → 第二次请求 200（want 429）；② `readyz` 恒 200 → down 场景 200（want 503）；③ `rule_not_found` 改 400 → 错误码对拍红。
> - **未执行（如实，不写成已完成）**：⑤①「真实 PG 断开 → 恢复」与 ⑤②「`docker compose up --wait` 到 healthy」**未跑**——本机 Docker 引擎未起、`127.0.0.1:5445` 无监听；`/readyz` 的 PG 分支只由注入式用例覆盖，compose 探针只改了文本未实际拉起。据此本单记 🔶 而非 ✅。补齐条件：起临时 PG（约定端口 5445、容器名带 `tellurion-risk` 前缀）后跑 `compose up --wait` + 断连/恢复三态。

### SVC-RISK-G2 · 服务就绪门 G2：`risk.*` 唯一 writer 与可重放迁移（ADR-036）[P0] 🔶（2026-11-07 施工；真 PG 重放腿未执行）

- **① 目标**：`risk.*` 五族表（规则 / 版本 / 审批 / 审计 / `risk_events` / 熔断 + `limits`）在本仓自证「只有我写、迁移可重放」，并把「裁决权双真相」这条非表形态的缺口登记清楚。
- **② 上下文**：`internal/pgstore` 在 `cmd/riskservice` 启动时执行 `rule.Migrations()`（幂等 DDL），读-改-写经 `Store.Tx`（PG 行锁）；`risk.limits` 今天仍是**空壳**（本仓台账 `:564` 自证「`risk.limits` 若要端点则另立单」）；ADR-036 §5 的数据归属矩阵把 risk 这一行写成「规则快照已住 `risk.*`，但 `bundle → RiskPolicy` 未接，进程内限额仍来自 core 配置」⇒ **G2 对 risk 不只是表，还包括「限额真相是否唯一」**（这是六仓里唯一有此形态的仓）；`risk.audit_logs`（人操作审计）与 core 的 `audit.message_log`（消息重放）**不是一回事**，护栏别把它们当一个。
- **③ 要求**：① 迁移重放自证：同一测试库连跑两次 `rule.Migrations()`，行数 / `pg_indexes` / 列集指纹逐位不变；② 禁跨 schema 写护栏：静态扫本仓 SQL，出现 `quant.` / `market.` / `public.audit_logs` 等**非本仓 schema** 的写语句即红（`public.audit_logs` 属 core，必须豁免名单之外）；③ `risk.limits` 的处置：要么给出「谁写、写什么、消费方怎么读」并立端点，要么显式声明「本表暂不启用」并从 `db-schema.md` 的 owner 表里标注为未启用——**二选一在单内定死，不许留空壳**；④ 把「限额真相」这条按 ADR-036 §5.2 写成临时双 owner：当前写者是 core 的 `risk_manager.*` 配置，目标写者是本仓快照，收口条件指向 `SVC-CORE-RISK-RETIRE`。
- **④ 约束与非目标**：不 `DROP` 表；不改规则参数词典（QT-74 已 ✅，动它另单）；不接 core 的 `ParseBundle`（跨仓）；不新建与 core 重叠的审计表。
- **⑤ 验收**：① 重放腿两次指纹逐位相同（贴输出）；② 破坏验证：加一行 `INSERT INTO public.audit_logs` ⇒ 护栏红，还原复绿；③ `risk.limits` 的处置有落档且 `db-schema.md` 一致；④ 「限额真相」双 owner 行写进 `db-schema.md` 并可被 core 反查；⑤ 三条命令绿。
- **⑥ 边界**：本仓 schema 与真相登记；事件 `actor` 列的 schema 变更**不在本单**（见 `QT-80`，同为本仓但独立验收）。

> **施工记录（2026-11-07）**：
> - **① 迁移重放自证腿**：新增 `internal/pgstore/migrate_test.go`（`TestMigrationReplay_Integration`，读既有 `RISK_TEST_DSN`，未设置即 `t.Skip`）：同一测试库连跑两次 `pgstore.Migrate`（= `rule.Migrations()`），对「各表行数 / `pg_indexes` 的 indexname+indexdef / `information_schema.columns` 列集」三组**排序后 sha256** 指纹逐位比对，并断言 7 张表全部存在；另加不依赖 PG 的 `TestFingerprint_IsDeterministic`（顺序无关、内容变化必换指纹）。
> - **② 禁跨 schema 写护栏**：新增 `internal/pgstore/ownership_test.go`（`TestNoCrossSchemaWrites`）：用 `go/parser` 抽本仓全部 `.go` 的字符串常量（不扫注释），按写语句正则（INSERT / UPDATE / DELETE / MERGE / TRUNCATE / CREATE-ALTER-DROP / COPY）取 schema，命中 `quant.` / `market.` / `run.` / `strategy.` / `analysis.` / `ai.` / `broker.` / `public.` 即红；`risk.` / `pg_catalog` / `information_schema` / 无 schema 的临时表与全部只读 SELECT 放行。`public.audit_logs` 归 `public` 命中集，**不在豁免名单**（`risk.audit_logs` 与 core 的 `audit_logs` / `audit.message_log` 仍按 §1 的「两个 audit 不是一回事」区分）。附 `TestForeignWriteGuardrail_Fixtures` 正反例（正例用字符串拼接构造，避免自指）。
> - **③ `risk.limits` 处置 = 「显式声明本表暂不启用」**（二选一已定死）：`docs/db-schema.md` §1 owner 表标「**未启用、无写者**」，§7.4 写启用前置两条（本仓限额端点另立单 + 消费方 = `tellurion:SVC-CORE-RISK-RETIRE` 的 `bundle` 接线），§6 同口径。**未新建端点、未加写路径**。
> - **④ 「限额真相」双 owner**：`docs/db-schema.md` **新增 §8**（此前只有散落的 §1/§6/§7.4 定性，无登记行）：当前写者 = `tellurion:config/analysis-service.yaml` 的 `alert.*` 5 个阈值 + `risk_manager.*` 14 个标量（`tellurion:pkg/bootstrap/engine.go` 的 `BuildRiskManager` 读取）；目标写者 = 本仓 `rule_snapshots.rules[].params`（`risk.limits` 未启用、不在名单）；收口 = `tellurion:SVC-CORE-RISK-RETIRE` + 本仓 `SVC-RISK-G3`；`resolve_enabled` 今天仍为 `false`（`tellurion:config/analysis-service.yaml:122`）。
> - **验收（真实退出码）**：`gofmt -l` 改动文件 = 空输出（`GOFMT_EXIT=0`）；`BUILD_EXIT=0` / `VET_EXIT=0` / `TEST_EXIT=0`（6 个包全 `ok`；`pgstore` 含新增 4 条用例）。
> - **破坏验证（真做真还原）**：在 `internal/rule/ddl.go` 的 `migrationStatements` 临时插入一行 `` `INSERT INTO public.audit_logs (id) VALUES ('x')` ``（第 10 行）⇒ `TestNoCrossSchemaWrites` **FAIL**：`internal\rule\ddl.go:10 writes to foreign schema "public": INSERT INTO public.audit_logs (id) VALUES ('x')`（`DESTROY_EXIT=1`）。用备份逐字节还原后 `SHA256_BEFORE = SHA256_AFTER = CFF8ABCA6503163C8E4173F4063868556C2E125516EEF897BCF81B3F9DBF08AD`，复跑同用例 `PASS`（`RESTORE_TEST_EXIT=0`）。
> - **未执行（如实，不写成已完成）**：⑤①「真实 PG 上连跑两次 `rule.Migrations()` 指纹不变」**未跑**——本机无 PostgreSQL（`127.0.0.1:5432` 无监听）、Docker 引擎不可用；该腿只在 `RISK_TEST_DSN` 非空时执行，本轮 `go test` 里为 `--- SKIP: TestMigrationReplay_Integration (RISK_TEST_DSN not set)`，**不是假绿**。据此本单记 🔶。补齐条件：起临时 PG（容器名带 `tellurion-risk` 前缀）后跑 `RISK_TEST_DSN='postgres://...' go test ./internal/pgstore -run TestMigrationReplay -count=1 -v`。

### QT-80 · 风险事件行带调用方身份（`risk_events.actor` + 写审计）[P1] 🔶（2026-11-07 代码已落；PG 集成腿未执行）

- **① 目标**：让 `POST /api/risk/events` 收进来的每一条事件能回答「谁触发的」，并把这条变更作为 DDL 单独立执行。
- **② 上下文**：来源是本仓台账 `:422` 那句「要让事件带调用方身份 = 加列 + 加审计写（DDL 变更，**另立单**），不在本票范围」——那张单此前不存在；现状实测：`actor` 取自鉴权 claims 只作用于**规则 create/update/approve 三处**（ADR-0005），`POST /events` 今天**不写审计行**，`risk.risk_events` **没有 actor 列**；core 侧告警回传的 `verdict:"alert"` 事件（`tellurion:cmd/analysis/wiring/risk_events.go`）带的是 service 身份（`sub=service:core`），所以「actor」的取值域必须先定（服务名？还是事件里透传的原始触发者？）。
- **③ 要求**：① `risk.risk_events` 加 `actor` 列（可空 + 老行为主 `NULL`，不许回填编造值）；② 取值口径定死：优先 claims 的 `sub`（`service:<repo>` 或用户 id），open-access 记常量 `open-access`，与规则审计同一套（ADR-0005 已裁的姿势，不另造）；③ 决定是否同时写审计行（若写，`risk.audit_logs` 的事件类动作要进 `api-schema.md` 的审计读面）；④ `docs/db-schema.md` §事件表 + `api-schema.md` `POST /events` 响应/字段同步；⑤ 迁移走 `rule.Migrations()` 这条唯一路径，且与 G2 的重放腿同批。
- **④ 约束与非目标**：不改 core 侧的上报报文形状（要改先走 `contracts`，禁第四份手写 DTO）；不把 `actor` 变成授权依据（它是留痕，不是权限）；不回填历史行；不动规则管理面的鉴权档位。
- **⑤ 验收**：① 用例两条：带 service token 投事件 ⇒ `actor = service:core`；无 token（open-access 回环）⇒ `actor = open-access`，**不再为空串**；② 老行 `actor IS NULL` 且读面不报错；③ 迁移重放腿（连跑两次）指纹不变；④ `db-schema.md` / `api-schema.md` 与实现一致；⑤ 三条命令绿。
- **⑥ 边界**：本仓列与留痕；「限额真相唯一」归 `SVC-RISK-G2` / `SVC-CORE-RISK-RETIRE`。

> **施工记录（2026-11-07）**：
> - **取值域裁决（③②）**：**复用**既有 `httpapi.actorOf`（不另造）：`username` 优先、缺失退 `subject`、open-access 记常量 `open-access`。服务 token 没有 `username` ⇒ 落 `sub`（如 `service:core`），与票面「优先 sub」在服务身份上等价；人身份仍与规则审计同口径。
> - **③③「是否同时写审计行」= 不写**：`risk.audit_logs.action` 的 CHECK 只允许 `create/update/approve/deprecate`（规则动作），事件不是规则动作；要写须改 CHECK + 扩审计读面语义，属另一单。已在 `db-schema.md` / `api-schema.md` 写明这句判断。
> - **落点**：`rule/ddl.go` 建表加 `actor TEXT`（可空、无默认、不回填）+ `ALTER TABLE ... ADD COLUMN IF NOT EXISTS actor TEXT`（老库补列，同走 `Migrations()` 唯一路径）；`event.Record` 加 `Actor`；`Ingest(ctx, raws, actor)` 由 HTTP 层传 `actorOf(r)`；`pgstore.PutEvents` 写 `actor`（空串落 NULL），`QueryEvents` 用 `COALESCE(actor,'')` 回显。
> - **验收（真实输出）**：`gofmt -l .` 空输出、`go build ./...` / `go vet ./...` / `go test ./... -count=1` 全绿（6 个包 `ok`）。
> - **用例**：`event.TestIngest_RecordsCallerActor`；`httpapi.TestEvents_RECORDActorFromClaimsNotBody` 三条（`service-token-sub` → `service:core`、`user-username` → `alice`、`open-access` → `open-access`，且请求体里伪造的 `"actor":"forged"` 不生效）。
> - **破坏验证**：把 `ingestEvents` 的 `actorOf(r)` 换成常量 `"open-access"` → 两条子用例红（`DESTROY_EXIT=1`，报文 `actor = "open-access", want "service:core"`）；还原后复绿。
> - **未执行（如实）**：⑤②「老行 `actor IS NULL` 且读面不报错」、⑤③「迁移连跑两次指纹不变」需要真 PG——本机无库（`127.0.0.1:5445` 无监听、Docker 引擎未起），本轮**未跑**；用例侧仅由 DDL 文本与 `COALESCE` 保证，故本单记 🔶 不标 ✅。补齐条件：起临时 PG（约定端口 5445、容器名带 `tellurion-risk` 前缀）后跑 `Migrations()` 两遍 + 插老行读回。

### SVC-RISK-G4 · 服务就绪门 G4：开关默认值 · 回传失败语义 · runbook（ADR-036）[P1] ⬜

- **① 目标**：把「本仓不可达时 core 会怎样」「事件回传开关关着时监控端点的空集是设定还是故障」成文，并给出可执行的运维手册。
- **② 上下文**：core 侧两条开关出厂为 `false`（`config/analysis-service.yaml:122` 的 `resolve_enabled`；`events_enabled` 同批默认关），因此 `monitor/alerts` / `monitor/evaluate` 在默认部署下**必然是空集**——这不是缺陷而是设定，但今天只散落在两仓的施工记录里；本仓侧失败语义已有（全端点 fail-closed，未装配 Authorizer 时 **503 拒绝而非放行**，这是正确姿势）；缺 `/readyz`（属 `SVC-RISK-G1` ①）、缺超时/重试口径、缺 runbook（全 workspace `runbook` 只在 ADR-036 正文出现）。
- **③ 要求**：① 失败语义表：Authorizer 未装配 / PG 不可达 / 规则库为空 / `resolve` 无生效版本 / `bundle` 参数未知键五类，各给 HTTP 码与调用方动作；② core→risk 与 risk→(无出站) 的超时与重试口径写明（裁决权出仓不许走重试放大：`resolve` 在 run 前一次，重试语义属 core 侧 `QT-77` 的凭证与超时笔）；③ 「空集的两种含义」（未接入 vs 没触发）在 `api-schema.md` 里给**可判别**的返回（例如 `configured: false` 或显式错误码），这是本仓最容易说谎的地方；④ `docs/runbook.md`：起停、看审计、重放迁移、熔断人工解除、开关核对；⑤ 一次演练：停掉本仓 ⇒ core 的 `risk_rules_hash` 腿与告警 relay 腿都**响亮失败且不静默降级**（core 侧 `SVC-CORE-RISK-RETIRE` ④ 的护栏）。
- **④ 约束与非目标**：不引 APM/告警通道；不做高可用；不改 `events_enabled` / `resolve_enabled` 的出厂值（值本身归 `tellurion:QT-77` 与 `SVC-CORE-RISK-RETIRE` ④）；不把检测器搬进本仓（U7 已裁）。
- **⑤ 验收**：① 失败语义表五类各有用例或注明原因；② 「空集可判别」有一条用例断言响应里带得出判别位；③ 演练有真退出码（停服前后 core 侧各一次，输出贴进本单施工记录）；④ runbook 每条命令当场执行；⑤ 三条命令绿。
- **⑥ 边界**：本仓运维面与语义判别；跨仓开关值与凭证归 core 侧两单。

## 施工序（候选单，开工前必须展开成六字段）

> 这张表是**施工顺序**，不是任务台账：每条在执行前要展开成六字段单（目标/上下文/要求/约束/验收/边界），并写明冻结契约的路径与节号。
> 顺序有依赖，**不要跳级**：跳序的代价是返工（例：X1a 的协议没冻结就写 aggregator，等于把协议写死在一个服务里）。
> 2026-10-21：候选单已展开为「一、本仓任务」的正式六字段条目（表中「上游单号」指向的 QT 单正文现在就在本仓）。

| 阶段 | 内容 | 上游单号 | 前置 |
|---|---|---|---|
| X3a | 规则定义 + revision + 审批状态机（draft→approved→active→deprecated） | `QT-42` | 🔶 已实装（`internal/rule` + `internal/httpapi` 4 端点；PG Store / 迁移执行 / 事务边界已落）；鉴权已落（2026-10-25，ADR-0005）+ 服务间角色 `service` 已落（2026-10-25，ADR-0006：`resolve` / `events` 两条） |
| X3b | 快照内容寻址 + `resolve` / `bundle` | `QT-42` | 🔶 已实装（2026-10-23：`rule_snapshots` DDL + 三端点 + `ParseBundle` 边界；Memory/PG 双实现）；F3 订正（2026-10-25）：✅ `risk_rules_hash` 生产者已落真值（core QT-66），⬜ `bundle` → `ParseBundle` → `RiskPolicy` 半边未接 —— **未接的原因是词典缺失，前置 = `QT-74`（本仓，见上）**；顺序 **QT-74 → `Snapshot`/`Bundle` 迁 contracts 发版 → core 消费**，不可颠倒 |
| X3c | 事件回传（异步批量）+ 监控聚合（敞口 / 告警） | `QT-42` | ✅ 事件回传（2026-10-24：`risk.risk_events` + `POST /events`，200/207 + 去重键）+ 监控 3 端点（2026-10-24：只读 `risk_events` 聚合/报表，ADR-0004；鉴权闸 fail-closed）+ **生产侧已接线（2026-10-25，core 腿）**：core 告警按 `verdict:"alert"` 回传 ⇒ `alerts` / `evaluate` 不再是结构性空集（开关默认关；生产侧已改持久 outbox，2026-10-25 二批 core `tellurion:a53fe95`） |
| X3d | 熔断指令（幂等键） | `QT-42` | 🔶 已实装（2026-10-24：`risk.circuit_orders` + `POST /breakers`，幂等键 + 409）；执行方 / ACK 未落 |

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

---
status: active
last-verified: 2026-11-10
verified-by: SVC-RISK-G4 / SVC-RISK-G2 —— 起停 / 探针 / 审计读 / 空集判别 / 停服演练在本机（Windows PowerShell 5，内存档）实跑；2026-11-10 起真 PG 腿（本机 PostgreSQL 16.15 @ `127.0.0.1:5432`）已补齐：迁移重放、psql 审计读、psql 熔断指令读、`go test ./...`；Docker / compose 腿同日补跑通过（`up --wait` 到 healthy + 断连/恢复三态）
---

# Runbook — tellurion-risk

> 只讲**本仓**运维面：起停、审计、迁移、熔断人工解除、开关核对、空集判读。
> 跨仓开关值与凭证**不在这里改**：`resolve_enabled` / `events_enabled` 归 `tellurion:QT-77` 与
> `SVC-CORE-RISK-RETIRE` ④。本文件不复制别的仓的值。
> 命令口径：Linux 用 `curl`；Windows PowerShell 5 用 `curl.exe`（`curl` 是 `Invoke-WebRequest` 别名）。
> 端口：本仓 `cmd/riskservice` 默认 `127.0.0.1:8089`（ADR-036 G0）。

## 1. 起停

### 1.1 内存档（本地 / 排障；**重启即丢**，不得用于生产）

```bash
go build -o /tmp/riskservice ./cmd/riskservice
# 裸本地联调：无 secret 时必须显式声明第 3 态，否则写端点 503 auth_unavailable
AUTH_INSECURE_EXPOSURE=loopback-published /tmp/riskservice -addr 127.0.0.1:8089
# 另一个终端：
curl -sS http://127.0.0.1:8089/healthz   # {"status":"ok"}
curl -sS http://127.0.0.1:8089/readyz    # {"status":"ready"}
```

> 只起进程、不声明 `AUTH_INSECURE_EXPOSURE`：进程照样能起（第 2 态），但
> `POST/PUT /api/risk/*` 一律 `503 auth_unavailable`，只有读端点和
> `POST /api/risk/monitor/evaluate` 公开。这是 2026-11-12 起的有意行为。

### 1.2 PG 档

```bash
export RISK_DATABASE_URL='postgres://user:pass@host:5432/tellurion?sslmode=disable'
export JWT_SECRET='<与 core 共享的 secret>'   # 不设 + 非回环 = 启动闸拒绝启动
./riskservice -addr 127.0.0.1:8089
```

- 启动时 `pgstore.Migrate` 顺序执行 `rule.Migrations()`（幂等 DDL，**无版本表**）；PG 不可达 →
  进程 `log.Fatalf` 退出（非零），不会带着空 store 起。
- 非回环绑定必须给 `JWT_SECRET`；`-insecure-exposure` 只在显式本地开发时使用，且只接受 loopback。

### 1.3 鉴权三态（2026-11-12 起；六仓统一口径）

| 态 | 启动条件 | 写端点 | 读端点 | 启动日志 |
|---|---|---|---|---|
| 1 | `JWT_SECRET` 非空 | 需 `trader` / `admin`（`service` 只开 resolve / events） | 需 `viewer` | `auth=true, open-access=false` |
| 2 | 无 secret + 回环 + 无声明 | **一律 `503 auth_unavailable`** | 公开 | WARN：`authentication is not configured` |
| 3 | 无 secret + `AUTH_INSECURE_EXPOSURE=loopback-published`（任意绑定） | 放开（无鉴权） | 公开 | WARN：`写面无鉴权` + 绑定地址；非回环额外标 `已对外暴露` |

- 无 secret + 非回环 + 无声明 ⇒ **拒绝启动**（`log.Fatalf`，非零退出）。
- 第 3 态**不因绑定地址拒绝启动**：容器内一律绑 `0.0.0.0`，显式声明就是「我知道它在对外，
  且接受写面无鉴权」；绑定非回环时 WARN 必须出现「已对外暴露」。
- `AUTH_INSECURE=false` + 无 secret ⇒ 无论绑定地址一律拒绝启动（用于关闭第 2/3 态）。
- 第 2 态的存在是为了让「忘了配 secret」只降级写面，而不是把写面敞开。
- core 装配期会 `POST /api/risk/resolve`（写口径）：本地双进程联调时 risk 必须处于第 1 态
  （推荐）或显式第 3 态，否则 core 会因 503 拒绝启动（错误文案会给出两条出路）。

### 1.4 Docker Compose

```bash
docker compose -f deploy/compose.yaml up --build
docker compose -f deploy/compose.yaml ps
docker compose -f deploy/compose.yaml stop
```

- `[已执行 2026-11-10]` Docker 引擎 `29.8.0`：本机 PG 只放行 loopback，故用临时 override 把 `RISK_DATABASE_URL` 指向同网络内的 `postgres:16-alpine`（`depends_on: service_healthy`），
  `docker compose -f deploy/compose.yaml -f <override> up --build --wait` → **EXIT=0**，`pg` / `risk` 双 `healthy`；
  `/healthz` 200 `{"status":"ok"}`、`/readyz` 200 `{"status":"ready"}`；停 pg 后 `/readyz` 503，pg 恢复后 200（未重启 risk）。
  证据与完整输出见 `docs/TASKS.md` `SVC-RISK-G1` 2026-11-10 施工记录。

### 1.4 停服与确认

```bash
# 前台 Ctrl-C；或按 pid：
kill <pid>
curl -sS -m 2 http://127.0.0.1:8089/healthz   # 期望：连接拒绝，curl 退出码 7
```

- 停服是**硬失败**：core 的 `resolve` 腿拒绝开跑，事件腿交给持久 outbox 重投；
  本仓不提供「降级空集 / 回落默认限额」。
- 演练的两条腿与真实退出码见 §7。

### 1.5 出厂规则集 seed（2026-11-12 起）

启动时（迁移之后、开始监听之前）自动灌入 4 条出厂 `global` 规则，空库不再需要手写规则：

```bash
# 空库首次启动（PG 档）应看到：
# risk.rule_defs 出现 4 行
# 日志：factory seed v1 applied (created=[global-position-sizing-v1 global-regime-v1 global-stop-loss-v1 global-take-profit-v1] skipped=[])
# resolve：{"risk_rules_hash":"84a95d7d...","snapshot":{...}}

# 重复启动（幂等）：
# 日志：factory seed v1 applied (created=[] skipped=[global-position-sizing-v1 global-regime-v1 global-stop-loss-v1 global-take-profit-v1])
# 表行数不变（rule_defs=4 / rule_revisions=4），快照 hash 不变
```

- **只补缺、不覆盖**：`rule_id` 已存在（无论 draft / active、是否人工改过）一律跳过；人工修改后的版本、
  人工审批状态都保留。删掉某条出厂规则后重启，只会补回这一条，其它规则不动。
- **区分 seed 与人工规则**：seed 的 `create` / `approve` 审计 `actor` 固定为 `factory-seed`：

```sql
SELECT DISTINCT rule_id
  FROM risk.audit_logs
 WHERE action = 'create' AND actor = 'factory-seed';
```

  人工操作的 `actor` 来自鉴权 claims（无 secret 回环档为 `open-access`），不会等于该常量；
  人工改过的规则会追加人工身份的 `update` / `approve` / `deprecate` 行。
- **fail-closed**：seed 任一规则写入失败 ⇒ 进程非 0 退出，不会带着空规则集服务。
- 参数是工程默认值（2026-11-12 从部署库 1:1 捕获），**不是投资建议**；改参数走正常
  `PUT /api/risk/rules/{id}` + `POST .../approve`，不要改代码里的 seed 常量（改常量 = 改历史 hash 基线）。

## 2. 看审计

```bash
curl -sS "http://127.0.0.1:8089/api/risk/audit?rule_id=r-sl"
```

- 需要 viewer / admin 角色；loopback open-access 记 `actor="open-access"`。
- 返回 `{"audit":[...],"count":N}`；`action` 只可能是 `create|update|approve|deprecate`。
- PG 直查（只读；排障用）：

```bash
psql "$RISK_DATABASE_URL" -c "SELECT seq,rule_id,revision,action,actor,at FROM risk.audit_logs WHERE rule_id='r-sl' ORDER BY seq"
```

- 审计是**人操作留痕**；`POST /events` 不写这张表（事件自己的 actor 在 `risk.risk_events.actor`）。
- `[已执行 2026-11-10]` psql 腿在本机 PostgreSQL 16.15（库 `tellurion_risk_test`，服务 `127.0.0.1:18093`
  以 `RISK_DATABASE_URL` 连真库）上跑通：建 `r-sl` → 201、审批 → 200，psql 读回两行
  `5|r-sl|1|create|open-access` 与 `6|r-sl|1|approve|open-access`，退出码 0。

## 3. 重放迁移

- 迁移**无版本表**，每次启动都重放；「重放」= 重启进程。幂等由 `rule.Migrations()` 的
  `IF NOT EXISTS` 保证（ADR-0003）。

```bash
# 重放：重启即可
docker compose -f deploy/compose.yaml restart risk
# 或前台进程 kill 后重新启动二进制

# 核对表已存在
psql "$RISK_DATABASE_URL" -c "SELECT to_regclass('risk.rule_defs'), to_regclass('risk.risk_events'), to_regclass('risk.audit_logs')"
```

- 真库腿统一入口（唯一位：`RISK_TEST_DSN`，指向 `*_test` 库，禁止活库）：

```bash
RISK_TEST_DSN='postgres://user:pass@127.0.0.1:5432/tellurion_risk_test?sslmode=disable' \
  go test ./internal/pgstore -count=1 -v
```

- 覆盖四条腿：`TestMigrationReplay_Integration` / `TestQueryEvents_Integration` /
  `TestLegacyActorNullRow_Integration` / `TestApprovePromotesRevisionOnRealPG`。
- 未设 `RISK_TEST_DSN` 时四条腿 **skip**（`SKIP: RISK_TEST_DSN not set`，不是假绿）。
- 旧名 `TELLURION_RISK_TEST_DSN` 已废弃：设它不会再跑任何用例（避免「设一个名只跑一半腿」的假绿）。
- `[已执行 2026-11-10]` 本机 PostgreSQL 16.15（`127.0.0.1:5432`，库 `tellurion_risk_test`）：
  `RISK_TEST_DSN=... go test ./internal/pgstore -count=1 -v` → 全部 PASS（含 `TestMigrationReplay_Integration`），
  重放指纹 `rows=0829cbeb6b6a8ba6375d827f007dded6032691dec8b39d4c72fd29271cb183ff` /
  `indexes=65c95b0b7fe2dde6ba482b7388f2669a64202afb61cb2cdbc08f7512d2a3d529` /
  `columns=e60fe69414d3914b6c2ca3ec4123de0140b062ac69e7bd5ca91724647ab75075`，退出码 0；
  同 DSN 下 `go test ./... -count=1` 6 包全 ok、退出码 0。
- 禁止手写 `ALTER` 后不回写 `internal/rule/ddl.go`：DDL 唯一真相源是 `rule.Migrations()`。

## 4. 熔断：人工解除

**今天本仓只有下发，没有解除。** `POST /api/risk/breakers` 的冻结动作只有 `halt` / `cancel_all`；
`risk.circuit_orders` 是 append-only 指令日志，`status` 只写 `applied`，既无 release 动作也无执行方 ACK。

```bash
# 查最近指令
psql "$RISK_DATABASE_URL" -c "SELECT breaker_id,action,scope,reason,command_ts,status,received_at FROM risk.circuit_orders ORDER BY received_at DESC LIMIT 20"

# 下发（新 breaker_id；同键同载荷重放 = already_applied）
curl -sS -X POST http://127.0.0.1:8089/api/risk/breakers \
  -H 'Content-Type: application/json' \
  -d '{"breaker_id":"manual-20261107-1","action":"halt","scope":"global","reason":"drill"}'
```

**人工解除步骤（如实）**：

1. 在**执行侧**（broker / core 执行方）按其 runbook 恢复交易——本仓没有可点的解除端点。
2. 在 `risk.circuit_orders` 找到触发那条，把 `breaker_id` / 时间 / 依据抄进运维记录。
3. **不要** `UPDATE` / `DELETE` `circuit_orders`（append-only；改了就对不上历史）。
4. 若要让「解除」成为本仓一等动作：另立单扩 `action` 枚举（MINOR）+ 执行方 ACK 语义 + ADR；
   **不在故障现场顺手加**。

- `[已执行 2026-11-10]` 真库演练：`POST /breakers`（`breaker_id=manual-20261110-1`、`halt`/`global`、
  带 `ts`）→ 200 `{"status":"applied"}`；psql 读回 `manual-20261110-1|halt|global|drill|2026-11-10 11:00:00+01|applied`，
  退出码 0。`ts` 是必填字段（缺它 → 400 `invalid_params`），这是本轮实测撞到的第一处口径。

## 5. 开关核对（变更后 / 告警空集时）

1. **core 侧（只读，不在本仓改）**——文件 `tellurion:config/analysis-service.yaml`
   （本机 checkout 为 `../tellurion/config/analysis-service.yaml`）：

```bash
grep -nE 'resolve_enabled|events_enabled|resolve_url|events_url' ../tellurion/config/analysis-service.yaml
# PowerShell:
# Select-String -Path ..\tellurion\config\analysis-service.yaml -Pattern 'resolve_enabled|events_enabled|resolve_url|events_url'
```

   出厂都是 `false`；`resolve_url` 必须指向本仓**实际监听地址**（本仓默认 **8089**）。
   ⚠️ 已知跨仓口径不一致：core 注释仍写「tellurion-risk 默认 :8086」，本仓 `-addr` 默认是 8089（ADR-036 G0）。
   以本仓代码为准；改 core 注释不在本单。

2. **本仓侧声明**（必须与 core 实际开关一致）：

```bash
RISK_EVENTS_EXPECTED=true ./riskservice -addr 127.0.0.1:8089
# 启动日志应出现：event ingest declared=true (RISK_EVENTS_EXPECTED)
```

   `RISK_EVENTS_EXPECTED` 缺省 `false`，与 core 出厂 `events_enabled=false` 对齐。它只是**声明**，
   不是探测——本仓看不见 core 进程内的开关。

3. **读判别位**：

```bash
curl -sS "http://127.0.0.1:8089/api/risk/monitor/alerts?from=2026-11-01T00:00:00Z&to=2026-11-08T00:00:00Z"
```

   - `"configured":false` + 空 ⇒ **未接入**：查 core 的 `events_enabled` / `events_url` / token。
   - `"configured":true` + 空 ⇒ **没触发**：查 core 检测器是否触发、outbox 是否积压或 `failed`。
   - 非 2xx ⇒ **读失败**（查 `/readyz` 与 PG）；**不要**读成空集。

## 6. 空集怎么读

唯一口径见 `docs/api-schema.md` §1「空集的两种含义」：

| 响应 | 含义 | 动作 |
|---|---|---|
| `configured:false` + 空 | 未接入（声明为 false） | 核对开关 / URL / token；不要当成没触发 |
| `configured:true` + 空 | 已接入但窗口内没触发 | 查 core 生产侧与 outbox |
| 非 2xx | 读失败 | 查 `/readyz` / PG，按 5xx 处理 |

判别位在 `monitor/exposure`、`monitor/alerts`、`monitor/evaluate` 三个响应里都有，**必有键**。
用例：`TestMonitor_EmptySetIsDiscriminable` / `TestMonitor_PGUnreachableIsNotEmptySet`。

## 7. 停服演练（本仓侧，真实退出码）

```bash
# 1) 起一个可 resolve 的实例（内存档）
/tmp/riskservice -addr 127.0.0.1:18089 &
# 2) 建规则 + 审批（确保 resolve 能 200；命令见 api-schema §1）
# 3) 停
kill <pid>
# 4) 两条腿都应连接失败：curl 退出码 7，HTTP 000
curl -sS -o /dev/null -w '%{http_code}' -X POST http://127.0.0.1:18089/api/risk/resolve \
  -H 'Content-Type: application/json' -d '{"ruleset":"latest"}'; echo " exit=$?"
curl -sS -o /dev/null -w '%{http_code}' -X POST http://127.0.0.1:18089/api/risk/events \
  -H 'Content-Type: application/json' -d '{"events":[{"run_id":"run-1","order_id":"o-1","event_seq":1}]}'; echo " exit=$?"
```

- 期望：停服前 `resolve` 200 / `events` 200；停服后两条都 **curl exit 7 + HTTP 000**。
- **禁止**任何「回落默认限额 / 空集 200」的通过口径。
- 自动版：`go test ./internal/httpapi -run TestDrill_StoppedServiceFailsLoud -count=1 -v`。
- 本次真跑输出与退出码贴在 `docs/TASKS.md` G4 施工记录；core 侧那半**已于 2026-11-10 补齐**（core `RiskEventSink`
  + relay，停服前 `sent:1/accepted:1`、停服后三次失败后 outbox `status=failed, attempts=3`），证据见
  `docs/TASKS.md` `SVC-RISK-G3` 的 2026-11-10 施工记录 ④。

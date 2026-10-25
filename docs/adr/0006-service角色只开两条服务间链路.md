# 0006 · `service` 角色只开两条服务间链路（resolve / events）

- **状态**：已裁决（来源：裁-17，2026-10-25；派发块 G4）
- **场景**：鉴权接线落地后（ADR-0005），core 调本仓必须带 access token，否则 `POST /api/risk/resolve` 401 ⇒ 按裁-11 口径该 run 直接 `failed` + `risk_rules_unavailable`。裁-17 定稿用**专设 `service` 角色**的长期 token，否决复用 `trader`：`trader` 同时能下实盘单与 emergency-flatten，把这张 token 放进任意服务的配置等于把下单权复制出去（core `tellurion:docs/TASKS.md` QT-71 ②）。本仓腿 = 策略表给哪几条端点开这个角色。
- **决策**：
  1. **`service` 与人角色正交，不是「比 admin 低一档」**：kit 的角色判定是 any-of 且**不做排序**，`service` 不是档位表上的一行，而是与 `viewer` / `trader` / `admin` 并列的另一种调用主体。因此它必须**与 `trader` / `admin` 并列写进同一条 `Roles`**，不能替换它们。
  2. **只开两条端点**：`POST /api/risk/resolve`（run 前取快照）与 `POST /api/risk/events`（run 后回传）——这两条正是 core 代机器发起的链路（core `cmd/analysis/wiring/risk_rules.go` / `risk_events.go`）。
  3. **其余端点一律不开**：`GET /api/risk/audit`（人操作留痕，机器凭证不读）、规则管理面（`rules` / `approve`，人负责的动作）、`breakers` 与 `monitor/*` 一律不开。
  4. **新增档位不放松既有语义**：`viewer` 调 `resolve` 仍 403（`resolve` 是写口径，会落快照）；真人 `trader` / `admin` 的可达性不变。
  5. **守卫**：`TestPolicy_ServiceRoleScope`（service resolve 200 / events 200 / audit 403 / 管理面 403 / viewer 仍 403）、`TestPolicy_ServiceRoleDeclaredExactlyTwice`（service 只声明在这两条）、`TestPolicy_ServiceRoleCoexistsWithHumanRoles`（与真人角色同在一条 `Roles`），与既有的 `TestRouteTableEqualsPolicyTable` 双向对拍一起构成护栏。
- **理由**：为什么不是「给所有写端点开 service」——`breakers` 会下发停机 / 撤单指令，`rules` / `approve` 会改生效规则与审计留痕，这些都是**人**的责任动作；机器凭证能代劳它们等于把责任从人身上移走。为什么不是「复用 `trader`」——见场景里裁-17 的可证风险不对称。为什么 `audit` 不开——审计是「谁改了规则」的留痕，机器读它没有业务意义，反而扩大凭证可见面。
- **代价 / 后果**：① core 的服务间调用需要一张 `service` 角色的长期 access token（本仓**不发**、不存、不分发——签发与注入属 core QT-71 / kit KT-09）；② 本仓策略表从此承认第四种角色名，后续新增端点必须显式决定开不开 `service`，默认不开；③ `service` 能调 `resolve` ⇒ 它能触发 `latest` 快照落库（写口径），所以「服务间账号只有读权限」这种假设在本仓不成立；④ **本条只解了「档位」没解「token 形状」**：kit `auth/httpadapter.Authenticate` 要求 subject 可解析为 `int64`，而裁-17 的生产口径 `sub=service:<repo>` 非数字 ⇒ 生产 token 今天在本仓会被 **401** 挡在门外（实测见 `docs/TASKS.md` QT-42 施工记录的「跨仓缺口」条）——放宽 subject 校验属 kit 的机制语义，本仓改不得，已报协调者。
- **替代关系**：扩展 0005（鉴权接入：kit net/http 适配 + 本仓策略表）——机制（kit 适配 + 本仓策略表）不变，本条只定 `service` 的**适用范围**；0005 §3 的角色口径按本条补充。

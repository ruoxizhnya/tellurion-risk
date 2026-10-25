package httpapi

import kithttp "github.com/ruoxizhnya/tellurion-kit/auth/httpadapter"

// 本仓的「端点 × 最低角色」策略表（裁-15）。机制在 kit（token 解析 / kind=access /
// 角色 any-of），策略留本仓；新增 / 改名端点必须先改这里，再改路由表。
//
// 口径：读端点 viewer，写端点 trader，规则发布 / 审批 admin。角色是 any-of，
// 因此每档都要把更高角色显式列进来（kit 不做角色排序）。
//
// service 档（裁-17）：`service` 是**服务间**角色（core 代机器调用本仓），不是
// 「比 admin 低一档」的人角色——它与人角色正交，只在**两条服务间链路**开：
// `POST /api/risk/resolve`（run 前取快照）与 `POST /api/risk/events`（run 后回传）。
// `GET /api/risk/audit`（人操作留痕）与规则管理面（rules / approve）**不开**——
// 审计与审批是人负责的动作，不该由机器凭证代劳。
//
// ⚠️ 角色 any-of ⇒ **service 必须与 trader / admin 并列写进同一条 Roles**，
// 不能替换掉它们：kit 不做角色排序，`service` 不是一个「更高 / 更低」的位置。
// 少了 trader / admin 会让真人调用方被拒；少了 service 则 core 的服务间调用 403。
var riskPolicy = kithttp.MustNewPolicy(map[string]kithttp.Rule{
	// ── 规则管理：写 trader，审批 admin（人操作，不开 service）──
	"POST /api/risk/rules":              {Roles: []string{"trader", "admin"}},
	"PUT /api/risk/rules/{id}":          {Roles: []string{"trader", "admin"}},
	"POST /api/risk/rules/{id}/approve": {Roles: []string{"admin"}},
	"GET /api/risk/rules":               {Roles: []string{"viewer", "trader", "admin"}},

	// ── 快照 / 解析：读 viewer；resolve(latest) 会落快照，按写口径 trader ──
	// resolve 是服务间链路的一条（core 代机器取快照），故并列开 service。
	"POST /api/risk/resolve":                {Roles: []string{"trader", "admin", "service"}},
	"GET /api/risk/snapshots/{hash}":        {Roles: []string{"viewer", "trader", "admin"}},
	"GET /api/risk/snapshots/{hash}/bundle": {Roles: []string{"viewer", "trader", "admin"}},
	"GET /api/risk/audit":                   {Roles: []string{"viewer", "trader", "admin"}},

	// ── 事件回传 / 熔断：写 trader ──
	// events 是服务间链路的另一条（core 代机器回传事件），故并列开 service。
	"POST /api/risk/events":   {Roles: []string{"trader", "admin", "service"}},
	"POST /api/risk/breakers": {Roles: []string{"trader", "admin"}},

	// ── 监控：读 viewer；evaluate 只读不写库，按读口径 ──
	"GET /api/risk/monitor/exposure":  {Roles: []string{"viewer", "trader", "admin"}},
	"GET /api/risk/monitor/alerts":    {Roles: []string{"viewer", "trader", "admin"}},
	"POST /api/risk/monitor/evaluate": {Roles: []string{"viewer", "trader", "admin"}},
})

// PolicyEntries 返回策略表副本，供守卫测试与路由表双向对拍。
func PolicyEntries() map[string]kithttp.Rule { return riskPolicy.Entries() }

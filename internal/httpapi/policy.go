package httpapi

import kithttp "github.com/ruoxizhnya/tellurion-kit/auth/httpadapter"

// 本仓的「端点 × 最低角色」策略表（裁-15）。机制在 kit（token 解析 / kind=access /
// 角色 any-of），策略留本仓；新增 / 改名端点必须先改这里，再改路由表。
//
// 口径：读端点 viewer，写端点 trader，规则发布 / 审批 admin。角色是 any-of，
// 因此每档都要把更高角色显式列进来（kit 不做角色排序）。
var riskPolicy = kithttp.MustNewPolicy(map[string]kithttp.Rule{
	// ── 规则管理：写 trader，审批 admin ──
	"POST /api/risk/rules":              {Roles: []string{"trader", "admin"}},
	"PUT /api/risk/rules/{id}":          {Roles: []string{"trader", "admin"}},
	"POST /api/risk/rules/{id}/approve": {Roles: []string{"admin"}},
	"GET /api/risk/rules":               {Roles: []string{"viewer", "trader", "admin"}},

	// ── 快照 / 解析：读 viewer；resolve(latest) 会落快照，按写口径 trader ──
	"POST /api/risk/resolve":                {Roles: []string{"trader", "admin"}},
	"GET /api/risk/snapshots/{hash}":        {Roles: []string{"viewer", "trader", "admin"}},
	"GET /api/risk/snapshots/{hash}/bundle": {Roles: []string{"viewer", "trader", "admin"}},
	"GET /api/risk/audit":                   {Roles: []string{"viewer", "trader", "admin"}},

	// ── 事件回传 / 熔断：写 trader ──
	"POST /api/risk/events":   {Roles: []string{"trader", "admin"}},
	"POST /api/risk/breakers": {Roles: []string{"trader", "admin"}},

	// ── 监控：读 viewer；evaluate 只读不写库，按读口径 ──
	"GET /api/risk/monitor/exposure":  {Roles: []string{"viewer", "trader", "admin"}},
	"GET /api/risk/monitor/alerts":    {Roles: []string{"viewer", "trader", "admin"}},
	"POST /api/risk/monitor/evaluate": {Roles: []string{"viewer", "trader", "admin"}},
})

// PolicyEntries 返回策略表副本，供守卫测试与路由表双向对拍。
func PolicyEntries() map[string]kithttp.Rule { return riskPolicy.Entries() }

// Package risk 是 tellurion-risk 的仓级文档锚点。
//
// 本仓是风控规则的真相源（规则定义 / 版本 / 审批 / 审计），同步裁决留在 core 进程内。
// 实现按能力拆子包，根包不放代码：
//
//   - internal/rule       规则领域：冻结枚举、version_hash、Store 端口与 Memory 实现、DDL、审批状态机与审计
//   - internal/httpapi    规则管理的 4 个 HTTP 端点（严格解码 + 错误码映射）
//   - cmd/riskservice     服务入口
//
// 本仓当前零跨仓依赖（contracts / kit 均未引入），单独 checkout 即可 go build / vet / test。
package risk

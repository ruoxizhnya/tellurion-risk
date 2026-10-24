// Package risk 是 tellurion-risk 的仓级文档锚点。
//
// 本仓是风控规则的真相源（规则定义 / 版本 / 审批 / 审计），同步裁决留在 core 进程内。
// 实现按能力拆子包，根包不放代码：
//
//   - internal/rule       规则领域：冻结枚举、version_hash、Store 端口与 Memory 实现、DDL、审批状态机与审计
//   - internal/event      事件回传（去重落库）与监控聚合读取（exposure / alerts / evaluate）
//   - internal/breaker    熔断指令（幂等键）
//   - internal/pgstore    PostgreSQL 实现（rule / event / breaker + 监控读取）与启动迁移
//   - internal/httpapi    12 个 HTTP 端点（严格解码 + 错误码映射；monitor/* 过 Authorizer 闸）
//   - cmd/riskservice     服务入口
//
// 本仓当前零跨仓依赖（contracts / kit 均未引入），单独 checkout 即可 go build / vet / test。
package risk

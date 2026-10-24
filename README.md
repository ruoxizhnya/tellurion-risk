# tellurion-risk

> module: `tellurion-risk` ｜ 状态：**🔶 首批实装（QT-42）——规则 / 快照 / 事件 / 熔断 / 监控 3 端点已落地；Authorizer（QT-50 ④）与 core 侧接线未接**

风控规则的**真相源**：规则定义、限额表、聚合监控与熔断。**同步裁决（`RiskPolicy`）留在 core 进程内**。

## 目录

| 路径 | 内容 |
|---|---|
| `cmd/riskservice/` | HTTP 服务入口（默认 `127.0.0.1:8086`；DSN 非空用 PG，否则 Memory） |
| `internal/rule/` | 规则领域：冻结枚举、`version_hash`、Store 端口 + `Memory`、DDL、`Service`（状态机 + 审计 + 快照） |
| `internal/event/` | 事件回传 + 去重落库；监控聚合读取（exposure / alerts / evaluate 投影） |
| `internal/breaker/` | 熔断指令 + 幂等键 |
| `internal/pgstore/` | PG 实现（rule / event / breaker + 监控读取）与启动迁移 |
| `internal/httpapi/` | 12 个端点 + 严格解码 + 错误码映射 + monitor/* 鉴权闸（标准库 `net/http`） |
| `docs/` | 本仓文档：台账 / 边界 / 决策 / 归档 —— 从这里进：`docs/README.md` |
| `AGENTS.md` | 本仓入口（构建、边界、禁止、依赖版本） |
| `doc.go` | 仓级文档锚点（无实现代码） |

## 快速开始

```bash
go build ./...
go vet ./...
go test ./...
```

起服务：

```bash
go run ./cmd/riskservice -addr 127.0.0.1:8086
```

`POST /api/risk/rules` → `PUT /api/risk/rules/{id}` → `POST /api/risk/rules/{id}/approve` → `GET /api/risk/rules?scope=&active=`。

`monitor/*` 三端点必须过鉴权闸；`Authorizer` 未装配时一律 `503 auth_unavailable`（fail-closed，不是匿名可读）。

本仓**单独 checkout 就能跑通**上面三条。需要别的仓源码才过 → 边界错了，不是加 `replace` 能解决的问题。

## 边界

**可以**：持有 `risk` schema 的 DDL 与规则版本；下发限额快照给 core 的进程内裁决层；聚合监控与熔断（跨 run 维度）

**不可以**：把逐笔 / 逐 bar 裁决做成远程调用（硬否决）；import core 的实现包；持有订单/持仓真相（那是 core）

## 文档

- 文档导航：`docs/README.md`
- 本仓台账：`docs/TASKS.md`（唯一；跨仓任务只留指针）
- 边界与依赖方向：`docs/ARCHITECTURE.md`
- 决策记录：`docs/adr/`

## 许可

MIT（见 `LICENSE`）。

# tellurion-risk

> module: `tellurion-risk` ｜ 状态：**预留（S0 占位，触发式启动）——尚未启动，无实现**

风控规则的**真相源**：规则定义、限额表、聚合监控与熔断。**同步裁决（`RiskPolicy`）留在 core 进程内**。

## 目录

| 路径 | 内容 |
|---|---|
| `docs/` | 本仓文档：台账 / 边界 / 决策 / 归档 —— 从这里进：`docs/README.md` |
| `AGENTS.md` | 本仓入口（构建、边界、禁止、依赖版本） |
| `doc.go` | 占位包（无实现） |

## 快速开始

```bash
go build ./...
go vet ./...
go test ./...
```

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

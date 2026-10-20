# tellurion-risk

> module: `tellurion-risk` ｜ 状态：**预留（触发式）**——当前为占位仓，**未启动**。触发条件同 X 线通则；启动前先立单。

风控规则的**真相源**：规则定义、限额表、聚合监控与熔断。**同步裁决（`RiskPolicy`）留在 core 进程内**。

两层结构（ADR-D20）：服务做真相源 + 聚合监控 + 熔断；进程内做每笔/每 bar 的同步裁决。只有前者能出仓。

## 边界

**可以**
- 持有 `risk` schema 的 DDL 与规则版本
- 下发限额快照给 core 的进程内裁决层

**不可以**
- 把逐笔/逐 bar 裁决做成远程调用（硬否决）
- 依赖 core 的实现包

## 依赖方向

```
tellurion-risk
```

- 契约与工具一律以 **tag** 依赖（`github.com/ruoxizhnya/tellurion-contracts` / `-kit`），
  **禁止 `replace` 指向仓外路径**（单仓不可构建）。
- `go.work` 一律不入库。

## 施工入口

本仓所有任务登记在 **`tellurion` 仓的 `docs/TASKS.md`**（单一台账，避免双写）。
在台账出现本仓的单号之前，**不要在此写实现**。

## 权威文档

| 主题 | 位置 |
|---|---|
| 仓清单与命名 | `tellurion:docs/design/qt-decoupling/repo-inventory.md` |
| 分仓拓扑与阶梯 | `tellurion:docs/design/qt-decoupling/repo-topology-and-naming.md` |
| P0 修复基线 | `tellurion:docs/design/qt-decoupling/p0-round2-review.md` |

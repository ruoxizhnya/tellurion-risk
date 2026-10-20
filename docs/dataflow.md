---
status: active
last-verified: 2026-10-20
verified-by: 功能边界固化（三份契约：db / api / dataflow）
---

# Dataflow — tellurion-risk

> 目的：把「数据从哪来、在谁手里变形、到哪去」写死。**任何跨仓数据交换都必须能在本图上找到一条边**；找不到 = 这段代码在偷偷建第二条路。

## 1. 主链路

```
人 / 审批
   │  rule → approve（新 version_hash）
   ↓
tellurion-risk  ── 写 risk.* ──►  （schema 唯一 owner）
   │  resolve / bundle（run 前 1 次）
   ↓
core 进程内 RiskPolicy  ◄── 每笔 CheckOrder（不走网络）
   │
   └── 异步事件回传 ──►  POST /api/risk/events ──►  risk.risk_events
```

## 2. 关键时序

### run 前（粗粒度，一次）
1. 调用方 `POST /api/risk/resolve`（`{rules: "latest"}` 或 `{hash}`）
2. 服务返回快照元数据（`risk_rules_hash`）
3. `GET /api/risk/snapshots/{hash}/bundle` 拉规则包 → 进程内构建 `RiskPolicy`
4. 该 hash 写入本次 run 的结果（可复现锚点）

### run 中（热路径，零网络）
5. 每笔 `CheckOrder(order)` → 进程内裁决 → 通过 / 拒绝 / 降规模
6. 事件进本地队列

### run 后（异步）
7. 批量 `POST /api/risk/events` → 落 `risk_events`
8. 监控层聚合敞口与告警；越限触发 `POST /breakers`

## 3. 失败模式（每一处 fail-closed 都要能指名）

| 失败点 | 症状 | 兜底 |
|---|---|---|
| 服务不可用 | 拿不到规则快照 | **拒绝开跑**（不许用默认限额） |
| 规则被改但 run 里没记 hash | 结果不可复现 | `risk_rules_hash` 必写进结果 |
| 事件回传失败 | 留痕缺失 | 本地队列 + 重试；允许延迟，不允许丢 |
| 熔断指令重复 | 重复平仓 | 幂等键 |

## 4. 不变量（可被测试钉住的）

1. 任一 run 的 `risk_rules_hash` 能反查到唯一快照。
2. 同一 hash 的规则包字节一致。
3. 热路径零 HTTP（可用调用栈测试钉住）。
4. 未审批规则不可能生效（状态机测试）。

## 5. 与契约文档的关系

- 表结构 → `docs/db-schema.md`
- 接口形状 → `docs/api-schema.md`
- 本文件只讲**流向与顺序**，不重复字段定义。

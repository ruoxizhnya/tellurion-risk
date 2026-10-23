# 0003 · PG Store 与事务边界

- **状态**：已裁决（2026-10-24，QT-42 PG Store 实装时钉住）
- **场景**：X3a/X3b 只有 `Memory` Store，Service 用一个进程内互斥锁串行「读-改-写」。这有两个硬问题：
  ① **多进程丢更新**：两个实例同时 approve 同一规则，都读到同一个 `head_revision` / `active_revision`，后写覆盖先写；
  ② **重启即丢**：规则真相源不能只活在内存里。另外 `rule.Migrations()` 已交副本，但没有执行入口，DDL 从未落库。
- **决策**：
  1. **Store 端口加事务**：`Tx(ctx, fn func(Store) error)`。Service 的所有写路径（`Create` / `Update` / `Approve` /
     `Resolve(latest)`）都在 `Tx` 内完成读-改-写；**删除 Service 的进程内互斥锁**，不保留「锁 + 事务」双保险。
  2. **PG 实现放 `internal/pgstore`**：`Tx` = `BEGIN` + 事务视图；事务内 `GetDefinition` 用
     `SELECT ... FOR UPDATE` 行锁，`fn` 返回错误即回滚。
  3. **Memory 模拟同一语义**：`Memory.Tx` 先做全量状态深拷贝，失败还原。单仓测试因此能验证「失败回滚」，
     不必依赖真实 PG。
  4. **迁移执行入口**：`pgstore.Migrate` 按序执行 `rule.Migrations()`；沿用取向 ①（数组 + 启动执行、幂等、无版本表）。
     `cmd/riskservice` 在 `-dsn` / `RISK_DATABASE_URL` 非空时先 Ping、再 Migrate，最后装配 PG Store；空 DSN 退回
     Memory 并打显式告警。
  5. **驱动只在进程入口引入**：`github.com/jackc/pgx/v5/stdlib` 由 `cmd/riskservice` 空白导入；领域包不感知驱动。
- **理由**：
  - 选行锁而不是「乐观并发 + 重试」：approve 的前提是「读到当前 head 再决定废弃哪条 active」，重试路径要重放审计顺序，
    复杂度高于一次 `FOR UPDATE`；管理面是低频操作，锁竞争不是瓶颈。
  - 选 `Store.Tx` 而不是在 Service 里注入 `*sql.DB`：领域层保持对数据库无感知，Memory 与 PG 走同一套测试。
  - 选「每次 Tx 深拷贝全量状态」：管理面写频率极低（人操作），换来与 PG 一致的回滚语义，测试价值大于拷贝成本。
  - 丢弃「进程锁 + 事务」双保险：进程锁在多实例下不成立，留着只会掩盖「忘了用 Tx」的实现错误。
- **代价 / 后果**：
  - `Resolve(latest)` 在事务里做 `ListDefinitions` + 逐条读 revision + 写快照；持锁时间随规则条数线性增长。
    规则集规模是「几十条」量级，可接受；若将来上千条，改为一次 join 查询，而不是拆事务。
  - Memory 的 Tx 是 O(状态大小) 深拷贝；只用于测试与本地开发。
  - 事务隔离级别取数据库默认（PG = READ COMMITTED）；跨规则的快照一致性靠「resolve 与 approve 都走同一把行锁路径」。
  - `rule.Migrations()` 仍是唯一 DDL 真相；新增表必须同步 `docs/db-schema.md`，禁止另建 `.sql` 镜像。
- **替代关系**：补强 ADR-0002 的欠账（「接 PG 后锁要换成事务」），不替代 0001 / 0002 的其它裁决。
package rule

// migrationStatements 是本仓 DDL 的唯一真相（取向 ①：Go 数组 + 启动时按序执行、幂等、
// 无版本表）。每条都必须能在空库上一次跑完，且可重入。
// 执行入口（PG 连接 + 启动时跑）尚未接线：当前只有 Memory Store。
// 结构变更必须同步 docs/db-schema.md，结构级变更还要立 ADR。
var migrationStatements = []string{
	`CREATE SCHEMA IF NOT EXISTS risk`,

	`CREATE TABLE IF NOT EXISTS risk.rule_defs (
		rule_id          TEXT PRIMARY KEY,
		scope            TEXT NOT NULL CHECK (scope IN ('global','account','strategy','symbol')),
		type             TEXT NOT NULL CHECK (type IN ('position_sizing','stop_loss','regime','take_profit')),
		description      TEXT NOT NULL DEFAULT '',
		head_revision    INTEGER NOT NULL CHECK (head_revision > 0),
		status           TEXT NOT NULL CHECK (status IN ('draft','approved','active','deprecated')),
		active_revision  INTEGER,
		created_at       TIMESTAMPTZ NOT NULL,
		updated_at       TIMESTAMPTZ NOT NULL,
		CHECK ((status = 'active') = (active_revision IS NOT NULL))
	)`,

	`CREATE TABLE IF NOT EXISTS risk.rule_revisions (
		rule_id      TEXT NOT NULL REFERENCES risk.rule_defs(rule_id),
		revision     INTEGER NOT NULL CHECK (revision > 0),
		scope        TEXT NOT NULL CHECK (scope IN ('global','account','strategy','symbol')),
		type         TEXT NOT NULL CHECK (type IN ('position_sizing','stop_loss','regime','take_profit')),
		params       JSONB NOT NULL,
		description  TEXT NOT NULL DEFAULT '',
		status       TEXT NOT NULL CHECK (status IN ('draft','approved','active','deprecated')),
		version_hash CHAR(64) NOT NULL,
		created_at   TIMESTAMPTZ NOT NULL,
		approved_at  TIMESTAMPTZ,
		PRIMARY KEY (rule_id, revision),
		UNIQUE (rule_id, version_hash),
		CHECK ((status IN ('approved','active','deprecated')) = (approved_at IS NOT NULL))
	)`,

	`CREATE TABLE IF NOT EXISTS risk.audit_logs (
		seq       BIGSERIAL PRIMARY KEY,
		rule_id   TEXT NOT NULL,
		revision  INTEGER NOT NULL,
		action    TEXT NOT NULL CHECK (action IN ('create','update','approve','deprecate')),
		actor     TEXT NOT NULL DEFAULT '',
		detail    JSONB,
		at        TIMESTAMPTZ NOT NULL
	)`,

	`CREATE INDEX IF NOT EXISTS rule_revisions_status_idx
		ON risk.rule_revisions (rule_id, status)`,

	`CREATE INDEX IF NOT EXISTS audit_logs_rule_idx
		ON risk.audit_logs (rule_id, seq)`,
}

// Migrations 返回迁移语句的副本（调用方不能改到内部数组）。
func Migrations() []string {
	out := make([]string, len(migrationStatements))
	copy(out, migrationStatements)
	return out
}

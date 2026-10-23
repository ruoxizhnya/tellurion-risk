// Package pgstore 是 risk 真相源的 PostgreSQL 实现（rule.Store）。
//
// 事务边界：Service 的写路径通过 Store.Tx 进入数据库事务；Tx 内的 GetDefinition
// 使用行锁（SELECT ... FOR UPDATE），保证多进程并发 approve 不丢更新
// （docs/adr/0003-pg-store-与事务边界.md）。
//
// 迁移：Migrate 按序执行 rule.Migrations() 的幂等 DDL（无版本表，取向 ①）。
package pgstore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"tellurion-risk/internal/rule"
)

// Store 在 *sql.DB 上实现 rule.Store。
type Store struct {
	db *sql.DB
}

var _ rule.Store = (*Store)(nil)

// New 构造 Store。db 的生命周期由调用方管理。
func New(db *sql.DB) *Store { return &Store{db: db} }

// Migrate 按序执行本仓全部幂等 DDL。每条独立执行；失败即停并指出语句序号。
func Migrate(ctx context.Context, db *sql.DB) error {
	for i, stmt := range rule.Migrations() {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("pgstore: migration statement %d: %w", i, err)
		}
	}
	return nil
}

// querier 同时由 *sql.DB 与 *sql.Tx 满足。
type querier interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// Tx 开启数据库事务并把事务视图交给回调；回调返回错误即回滚。
func (s *Store) Tx(ctx context.Context, fn func(rule.Store) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("pgstore: begin: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	if err := fn(&txStore{q: tx}); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("pgstore: commit: %w", err)
	}
	committed = true
	return nil
}

// txStore 是事务内的 rule.Store 视图。GetDefinition 加行锁。
type txStore struct {
	q querier
}

var _ rule.Store = (*txStore)(nil)

func (t *txStore) Tx(_ context.Context, fn func(rule.Store) error) error { return fn(t) }

const definitionCols = `rule_id, scope, type, description, head_revision, status, active_revision, created_at, updated_at`

type rowScanner interface {
	Scan(dest ...any) error
}

func scanDefinition(row rowScanner) (rule.Definition, error) {
	var (
		def    rule.Definition
		active sql.NullInt64
	)
	err := row.Scan(&def.RuleID, &def.Scope, &def.Type, &def.Description, &def.HeadRevision,
		&def.Status, &active, &def.CreatedAt, &def.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return rule.Definition{}, rule.ErrRuleNotFound
	}
	if err != nil {
		return rule.Definition{}, err
	}
	if active.Valid {
		def.ActiveRevision = int(active.Int64)
	}
	return def, nil
}

func getDefinition(ctx context.Context, q querier, ruleID string, forUpdate bool) (rule.Definition, error) {
	query := `SELECT ` + definitionCols + ` FROM risk.rule_defs WHERE rule_id = $1`
	if forUpdate {
		query += ` FOR UPDATE`
	}
	return scanDefinition(q.QueryRowContext(ctx, query, ruleID))
}

func (s *Store) GetDefinition(ctx context.Context, ruleID string) (rule.Definition, error) {
	return getDefinition(ctx, s.db, ruleID, false)
}

func (t *txStore) GetDefinition(ctx context.Context, ruleID string) (rule.Definition, error) {
	return getDefinition(ctx, t.q, ruleID, true)
}

func listDefinitions(ctx context.Context, q querier, f rule.Filter, forUpdate bool) ([]rule.Definition, error) {
	query := `SELECT ` + definitionCols + ` FROM risk.rule_defs`
	var (
		conds []string
		args  []any
	)
	if f.Scope != "" {
		args = append(args, string(f.Scope))
		conds = append(conds, fmt.Sprintf("scope = $%d", len(args)))
	}
	if f.Active {
		conds = append(conds, `status = 'active'`)
	}
	if len(conds) > 0 {
		query += ` WHERE ` + strings.Join(conds, ` AND `)
	}
	query += ` ORDER BY rule_id`
	if forUpdate {
		// resolve(latest) 用：锁住参与快照的定义行，避免与并发 approve 交错出跨规则的半套状态。
		query += ` FOR UPDATE`
	}
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]rule.Definition, 0)
	for rows.Next() {
		def, err := scanDefinition(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, def)
	}
	return out, rows.Err()
}

func (s *Store) ListDefinitions(ctx context.Context, f rule.Filter) ([]rule.Definition, error) {
	return listDefinitions(ctx, s.db, f, false)
}

func (t *txStore) ListDefinitions(ctx context.Context, f rule.Filter) ([]rule.Definition, error) {
	return listDefinitions(ctx, t.q, f, true)
}

func nullableInt(v int) any {
	if v == 0 {
		return nil
	}
	return v
}

func nullableTime(t *time.Time) any {
	if t == nil {
		return nil
	}
	return *t
}

func insertDefinition(ctx context.Context, q querier, def rule.Definition) error {
	_, err := q.ExecContext(ctx, `
		INSERT INTO risk.rule_defs
			(rule_id, scope, type, description, head_revision, status, active_revision, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		def.RuleID, string(def.Scope), string(def.Type), def.Description, def.HeadRevision,
		string(def.Status), nullableInt(def.ActiveRevision), def.CreatedAt, def.UpdatedAt)
	return mapPgError(err)
}

func updateDefinition(ctx context.Context, q querier, def rule.Definition) error {
	res, err := q.ExecContext(ctx, `
		UPDATE risk.rule_defs
		SET scope = $2, type = $3, description = $4, head_revision = $5,
		    status = $6, active_revision = $7, updated_at = $8
		WHERE rule_id = $1`,
		def.RuleID, string(def.Scope), string(def.Type), def.Description, def.HeadRevision,
		string(def.Status), nullableInt(def.ActiveRevision), def.UpdatedAt)
	if err != nil {
		return mapPgError(err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return rule.ErrRuleNotFound
	}
	return nil
}

func insertRevision(ctx context.Context, q querier, rev rule.Revision) error {
	_, err := q.ExecContext(ctx, `
		INSERT INTO risk.rule_revisions
			(rule_id, revision, scope, type, params, description, status, version_hash, created_at, approved_at)
		VALUES ($1, $2, $3, $4, $5::jsonb, $6, $7, $8, $9, $10)`,
		rev.RuleID, rev.Revision, string(rev.Scope), string(rev.Type), string(rev.Params),
		rev.Description, string(rev.Status), rev.VersionHash, rev.CreatedAt, nullableTime(rev.ApprovedAt))
	return mapPgError(err)
}

func (s *Store) CreateRule(ctx context.Context, def rule.Definition, rev rule.Revision) error {
	return createRule(ctx, s.db, def, rev)
}

func (t *txStore) CreateRule(ctx context.Context, def rule.Definition, rev rule.Revision) error {
	return createRule(ctx, t.q, def, rev)
}

func createRule(ctx context.Context, q querier, def rule.Definition, rev rule.Revision) error {
	if err := insertDefinition(ctx, q, def); err != nil {
		return err
	}
	return insertRevision(ctx, q, rev)
}

func (s *Store) UpdateRule(ctx context.Context, def rule.Definition, rev rule.Revision) error {
	return updateRule(ctx, s.db, def, rev)
}

func (t *txStore) UpdateRule(ctx context.Context, def rule.Definition, rev rule.Revision) error {
	return updateRule(ctx, t.q, def, rev)
}

func updateRule(ctx context.Context, q querier, def rule.Definition, rev rule.Revision) error {
	if err := updateDefinition(ctx, q, def); err != nil {
		return err
	}
	return insertRevision(ctx, q, rev)
}

const revisionCols = `rule_id, revision, scope, type, params, description, status, version_hash, created_at, approved_at`

func scanRevision(row rowScanner) (rule.Revision, error) {
	var (
		rev        rule.Revision
		params     []byte
		approvedAt sql.NullTime
	)
	err := row.Scan(&rev.RuleID, &rev.Revision, &rev.Scope, &rev.Type, &params,
		&rev.Description, &rev.Status, &rev.VersionHash, &rev.CreatedAt, &approvedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return rule.Revision{}, rule.ErrRevisionNotFound
	}
	if err != nil {
		return rule.Revision{}, err
	}
	rev.Params = append(json.RawMessage(nil), params...)
	if approvedAt.Valid {
		at := approvedAt.Time
		rev.ApprovedAt = &at
	}
	return rev, nil
}

func getRevision(ctx context.Context, q querier, ruleID string, revision int) (rule.Revision, error) {
	rev, err := scanRevision(q.QueryRowContext(ctx,
		`SELECT `+revisionCols+` FROM risk.rule_revisions WHERE rule_id = $1 AND revision = $2`,
		ruleID, revision))
	if errors.Is(err, rule.ErrRevisionNotFound) {
		// 区分「规则不存在」与「revision 不存在」：与 Memory 语义一致。
		if _, defErr := getDefinition(ctx, q, ruleID, false); errors.Is(defErr, rule.ErrRuleNotFound) {
			return rule.Revision{}, rule.ErrRuleNotFound
		}
	}
	return rev, err
}

func listRevisions(ctx context.Context, q querier, ruleID string) ([]rule.Revision, error) {
	rows, err := q.QueryContext(ctx,
		`SELECT `+revisionCols+` FROM risk.rule_revisions WHERE rule_id = $1 ORDER BY revision`, ruleID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]rule.Revision, 0)
	for rows.Next() {
		rev, err := scanRevision(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, rev)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		if _, defErr := getDefinition(ctx, q, ruleID, false); errors.Is(defErr, rule.ErrRuleNotFound) {
			return nil, rule.ErrRuleNotFound
		}
	}
	return out, nil
}

func (s *Store) GetRevision(ctx context.Context, ruleID string, revision int) (rule.Revision, error) {
	return getRevision(ctx, s.db, ruleID, revision)
}

func (t *txStore) GetRevision(ctx context.Context, ruleID string, revision int) (rule.Revision, error) {
	return getRevision(ctx, t.q, ruleID, revision)
}

func (s *Store) ListRevisions(ctx context.Context, ruleID string) ([]rule.Revision, error) {
	return listRevisions(ctx, s.db, ruleID)
}

func (t *txStore) ListRevisions(ctx context.Context, ruleID string) ([]rule.Revision, error) {
	return listRevisions(ctx, t.q, ruleID)
}

func putRevision(ctx context.Context, q querier, rev rule.Revision) error {
	res, err := q.ExecContext(ctx, `
		UPDATE risk.rule_revisions
		SET status = $3, approved_at = $4
		WHERE rule_id = $1 AND revision = $2 AND version_hash = $5`,
		rev.RuleID, rev.Revision, string(rev.Status), nullableTime(rev.ApprovedAt), rev.VersionHash)
	if err != nil {
		return mapPgError(err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	var existingHash string
	err = q.QueryRowContext(ctx,
		`SELECT version_hash FROM risk.rule_revisions WHERE rule_id = $1 AND revision = $2`,
		rev.RuleID, rev.Revision).Scan(&existingHash)
	if errors.Is(err, sql.ErrNoRows) {
		if _, defErr := getDefinition(ctx, q, rev.RuleID, false); errors.Is(defErr, rule.ErrRuleNotFound) {
			return rule.ErrRuleNotFound
		}
		return rule.ErrRevisionNotFound
	}
	if err != nil {
		return err
	}
	return rule.ErrRevisionConflict
}

func (s *Store) PutRevision(ctx context.Context, rev rule.Revision) error {
	return putRevision(ctx, s.db, rev)
}

func (t *txStore) PutRevision(ctx context.Context, rev rule.Revision) error {
	return putRevision(ctx, t.q, rev)
}

func appendAudit(ctx context.Context, q querier, entry rule.AuditEntry) (int64, error) {
	var detail any
	if len(entry.Detail) > 0 {
		detail = string(entry.Detail)
	}
	var seq int64
	err := q.QueryRowContext(ctx, `
		INSERT INTO risk.audit_logs (rule_id, revision, action, actor, detail, at)
		VALUES ($1, $2, $3, $4, $5::jsonb, $6)
		RETURNING seq`,
		entry.RuleID, entry.Revision, entry.Action, entry.Actor, detail, entry.At).Scan(&seq)
	return seq, err
}

func (s *Store) AppendAudit(ctx context.Context, entry rule.AuditEntry) (int64, error) {
	return appendAudit(ctx, s.db, entry)
}

func (t *txStore) AppendAudit(ctx context.Context, entry rule.AuditEntry) (int64, error) {
	return appendAudit(ctx, t.q, entry)
}

func listAudit(ctx context.Context, q querier, ruleID string) ([]rule.AuditEntry, error) {
	rows, err := q.QueryContext(ctx, `
		SELECT seq, rule_id, revision, action, actor, detail, at
		FROM risk.audit_logs WHERE rule_id = $1 ORDER BY seq`, ruleID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]rule.AuditEntry, 0)
	for rows.Next() {
		var (
			entry  rule.AuditEntry
			detail []byte
		)
		if err := rows.Scan(&entry.Seq, &entry.RuleID, &entry.Revision, &entry.Action,
			&entry.Actor, &detail, &entry.At); err != nil {
			return nil, err
		}
		if detail != nil {
			entry.Detail = append(json.RawMessage(nil), detail...)
		}
		out = append(out, entry)
	}
	return out, rows.Err()
}

func (s *Store) ListAudit(ctx context.Context, ruleID string) ([]rule.AuditEntry, error) {
	return listAudit(ctx, s.db, ruleID)
}

func (t *txStore) ListAudit(ctx context.Context, ruleID string) ([]rule.AuditEntry, error) {
	return listAudit(ctx, t.q, ruleID)
}

func putSnapshot(ctx context.Context, q querier, snap rule.Snapshot) error {
	derived, err := rule.SnapshotHash(snap.Rules)
	if err != nil {
		return err
	}
	if derived != snap.RiskRulesHash {
		return fmt.Errorf("%w: derived %s, claimed %q", rule.ErrSnapshotMismatch, derived, snap.RiskRulesHash)
	}
	rulesJSON, err := json.Marshal(snap.Rules)
	if err != nil {
		return fmt.Errorf("pgstore: marshal snapshot rules: %w", err)
	}
	res, err := q.ExecContext(ctx, `
		INSERT INTO risk.rule_snapshots (risk_rules_hash, rules, created_at)
		VALUES ($1, $2::jsonb, $3)
		ON CONFLICT (risk_rules_hash) DO NOTHING`,
		snap.RiskRulesHash, string(rulesJSON), snap.CreatedAt)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	// 同 hash 已存在：内容重新 derive 必须仍等于该 hash；否则是内容寻址被破坏。
	stored, _, err := readSnapshot(ctx, q, snap.RiskRulesHash)
	if err != nil {
		return err
	}
	storedHash, err := rule.SnapshotHash(stored)
	if err != nil {
		return fmt.Errorf("%w: stored snapshot %s is corrupted: %v",
			rule.ErrSnapshotMismatch, snap.RiskRulesHash, err)
	}
	if storedHash != snap.RiskRulesHash {
		return fmt.Errorf("%w: hash %s already stored with different content (derived %s)",
			rule.ErrSnapshotMismatch, snap.RiskRulesHash, storedHash)
	}
	return nil
}

func (s *Store) PutSnapshot(ctx context.Context, snap rule.Snapshot) error {
	return putSnapshot(ctx, s.db, snap)
}

func (t *txStore) PutSnapshot(ctx context.Context, snap rule.Snapshot) error {
	return putSnapshot(ctx, t.q, snap)
}

func readSnapshot(ctx context.Context, q querier, hash string) ([]rule.SnapshotRule, time.Time, error) {
	var (
		rules     []byte
		createdAt time.Time
	)
	err := q.QueryRowContext(ctx,
		`SELECT rules, created_at FROM risk.rule_snapshots WHERE risk_rules_hash = $1`, hash).
		Scan(&rules, &createdAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, time.Time{}, rule.ErrSnapshotNotFound
	}
	if err != nil {
		return nil, time.Time{}, err
	}
	var out []rule.SnapshotRule
	if err := json.Unmarshal(rules, &out); err != nil {
		return nil, time.Time{}, fmt.Errorf("pgstore: decode snapshot %s: %w", hash, err)
	}
	return out, createdAt, nil
}

func getSnapshot(ctx context.Context, q querier, hash string) (rule.Snapshot, error) {
	rules, createdAt, err := readSnapshot(ctx, q, hash)
	if err != nil {
		return rule.Snapshot{}, err
	}
	derived, err := rule.SnapshotHash(rules)
	if err != nil {
		return rule.Snapshot{}, fmt.Errorf("%w: stored snapshot %s is corrupted: %v",
			rule.ErrSnapshotMismatch, hash, err)
	}
	if derived != hash {
		return rule.Snapshot{}, fmt.Errorf("%w: derived %s, stored under %q",
			rule.ErrSnapshotMismatch, derived, hash)
	}
	return rule.Snapshot{RiskRulesHash: hash, Rules: rules, CreatedAt: createdAt}, nil
}

func (s *Store) GetSnapshot(ctx context.Context, hash string) (rule.Snapshot, error) {
	return getSnapshot(ctx, s.db, hash)
}

func (t *txStore) GetSnapshot(ctx context.Context, hash string) (rule.Snapshot, error) {
	return getSnapshot(ctx, t.q, hash)
}

// mapPgError 把唯一约束冲突翻译成领域哨兵；其他错误原样返回。
func mapPgError(err error) error {
	if err == nil {
		return nil
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		switch pgErr.ConstraintName {
		case "rule_defs_pkey":
			return fmt.Errorf("%w: %s", rule.ErrIdempotencyConflict, pgErr.ConstraintName)
		case "rule_revisions_pkey", "rule_revisions_rule_id_version_hash_key":
			return fmt.Errorf("%w: %s", rule.ErrRevisionConflict, pgErr.ConstraintName)
		}
	}
	return err
}

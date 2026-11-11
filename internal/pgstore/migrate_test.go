package pgstore

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// TestMigrationReplay_Integration 是本单（SVC-RISK-G2）的迁移重放自证腿：同一测试库连跑
// 两次 pgstore.Migrate（= rule.Migrations()），断言「各表行数 / pg_indexes 的
// indexname+indexdef 集合 / information_schema.columns 列集」三组指纹逐位不变。
//
// 可选 PG 腿（无库时 skip，不是假绿）：
//
//	RISK_TEST_DSN='postgres://postgres:postgres@127.0.0.1:5432/tellurion_risk_test?sslmode=disable' \
//	  go test ./internal/pgstore -run TestMigrationReplay -count=1
func TestMigrationReplay_Integration(t *testing.T) {
	dsn := requireTestDSN(t)
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		t.Fatalf("ping: %v", err)
	}

	if err := Migrate(ctx, db); err != nil {
		t.Fatalf("first migrate: %v", err)
	}
	before, err := takeSchemaSnapshot(ctx, db)
	if err != nil {
		t.Fatalf("snapshot before replay: %v", err)
	}
	if missing := before.missingTables(wantRiskTables); len(missing) > 0 {
		t.Fatalf("migration did not create tables: %s", strings.Join(missing, ", "))
	}

	if err := Migrate(ctx, db); err != nil {
		t.Fatalf("second migrate (replay): %v", err)
	}
	after, err := takeSchemaSnapshot(ctx, db)
	if err != nil {
		t.Fatalf("snapshot after replay: %v", err)
	}

	for _, leg := range []struct {
		name string
		a, b []string
	}{
		{"table row counts", before.rows, after.rows},
		{"pg_indexes (indexname+indexdef)", before.indexes, after.indexes},
		{"column set", before.columns, after.columns},
	} {
		if fingerprintLines(leg.a) != fingerprintLines(leg.b) {
			t.Fatalf("%s fingerprint changed across replay:\nbefore=%s\nafter=%s\nbefore lines=%q\nafter lines=%q",
				leg.name, fingerprintLines(leg.a), fingerprintLines(leg.b), leg.a, leg.b)
		}
	}

	t.Logf("replay fingerprints stable: rows=%s indexes=%s columns=%s",
		fingerprintLines(before.rows), fingerprintLines(before.indexes), fingerprintLines(before.columns))
}

// TestFingerprint_IsDeterministic 钉住指纹的确定性：与输入顺序无关，且内容变化必换指纹。
// 该用例不依赖 PG，随普通单测执行。
func TestFingerprint_IsDeterministic(t *testing.T) {
	a := []string{"b=2", "a=1", "c=3"}
	b := []string{"c=3", "a=1", "b=2"}
	if got, want := fingerprintLines(a), fingerprintLines(b); got != want {
		t.Fatalf("fingerprint must be order independent: %s != %s", got, want)
	}
	if fingerprintLines(a) == fingerprintLines([]string{"a=1", "b=2"}) {
		t.Fatal("fingerprint must change when content changes")
	}
	if got := fingerprintLines(nil); got != fingerprintLines([]string{}) {
		t.Fatalf("empty fingerprints must match: %s", got)
	}
}

// wantRiskTables 是本仓 risk schema 的全部表（docs/db-schema.md §1）。缺表就是迁移没跑全。
var wantRiskTables = []string{
	"audit_logs",
	"circuit_orders",
	"limits",
	"risk_events",
	"rule_defs",
	"rule_revisions",
	"rule_snapshots",
}

type schemaSnapshot struct {
	rows    []string
	indexes []string
	columns []string
}

func (s schemaSnapshot) missingTables(want []string) []string {
	seen := make(map[string]bool, len(s.rows))
	for _, line := range s.rows {
		if name, _, ok := strings.Cut(line, "|"); ok {
			seen[name] = true
		}
	}
	var missing []string
	for _, name := range want {
		if !seen[name] {
			missing = append(missing, name)
		}
	}
	return missing
}

// takeSchemaSnapshot 读 risk schema 的三组可复现观测：行数、索引定义、列定义。
func takeSchemaSnapshot(ctx context.Context, db *sql.DB) (schemaSnapshot, error) {
	var snap schemaSnapshot

	rows, err := db.QueryContext(ctx,
		`SELECT table_name FROM information_schema.tables
		 WHERE table_schema = 'risk' AND table_type = 'BASE TABLE'
		 ORDER BY table_name`)
	if err != nil {
		return snap, fmt.Errorf("list tables: %w", err)
	}
	var tables []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			rows.Close()
			return snap, fmt.Errorf("scan table name: %w", err)
		}
		tables = append(tables, name)
	}
	if err := rows.Close(); err != nil {
		return snap, fmt.Errorf("close table rows: %w", err)
	}
	if err := rows.Err(); err != nil {
		return snap, fmt.Errorf("iterate tables: %w", err)
	}

	for _, name := range tables {
		var count int64
		q := fmt.Sprintf(`SELECT count(*) FROM risk.%s`, quoteIdent(name))
		if err := db.QueryRowContext(ctx, q).Scan(&count); err != nil {
			return snap, fmt.Errorf("count risk.%s: %w", name, err)
		}
		snap.rows = append(snap.rows, fmt.Sprintf("%s|%d", name, count))
	}

	idxRows, err := db.QueryContext(ctx,
		`SELECT indexname, indexdef FROM pg_indexes WHERE schemaname = 'risk'`)
	if err != nil {
		return snap, fmt.Errorf("list indexes: %w", err)
	}
	for idxRows.Next() {
		var name, def string
		if err := idxRows.Scan(&name, &def); err != nil {
			idxRows.Close()
			return snap, fmt.Errorf("scan index: %w", err)
		}
		snap.indexes = append(snap.indexes, name+"|"+def)
	}
	if err := idxRows.Close(); err != nil {
		return snap, fmt.Errorf("close index rows: %w", err)
	}
	if err := idxRows.Err(); err != nil {
		return snap, fmt.Errorf("iterate indexes: %w", err)
	}

	colRows, err := db.QueryContext(ctx,
		`SELECT table_name, ordinal_position, column_name, data_type, is_nullable,
		        COALESCE(column_default, '')
		 FROM information_schema.columns
		 WHERE table_schema = 'risk'`)
	if err != nil {
		return snap, fmt.Errorf("list columns: %w", err)
	}
	for colRows.Next() {
		var (
			table, column, dataType, nullable, def string
			pos                                    int
		)
		if err := colRows.Scan(&table, &pos, &column, &dataType, &nullable, &def); err != nil {
			colRows.Close()
			return snap, fmt.Errorf("scan column: %w", err)
		}
		snap.columns = append(snap.columns,
			fmt.Sprintf("%s|%d|%s|%s|%s|%s", table, pos, column, dataType, nullable, def))
	}
	if err := colRows.Close(); err != nil {
		return snap, fmt.Errorf("close column rows: %w", err)
	}
	if err := colRows.Err(); err != nil {
		return snap, fmt.Errorf("iterate columns: %w", err)
	}

	return snap, nil
}

// fingerprintLines 先排序再去重地编码，最后取 sha256：同一集合永远同一指纹。
func fingerprintLines(lines []string) string {
	sorted := append([]string(nil), lines...)
	sort.Strings(sorted)
	h := sha256.New()
	for _, line := range sorted {
		h.Write([]byte(line))
		h.Write([]byte{'\n'})
	}
	return hex.EncodeToString(h.Sum(nil))
}

// quoteIdent 只用于本仓固定表名（来自 information_schema，不是外部输入）。
func quoteIdent(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

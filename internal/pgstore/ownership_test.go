package pgstore

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// foreignSchemaWriters 是本仓**禁止写入**的 schema（SVC-RISK-G2 ③②）。只有本仓的 `risk`
// schema 归本仓写；`public.audit_logs` 属 core，必须留在命中集里，不得进豁免名单。
var foreignSchemaWriters = map[string]bool{
	"quant":    true,
	"market":   true,
	"run":      true,
	"strategy": true,
	"analysis": true,
	"ai":       true,
	"broker":   true,
	"public":   true,
}

// writeStatementPatterns 只匹配**写语句**里的「schema.」；只读 SELECT 不在其中。
// 未带 schema 的语句（含 TEMP / pg_temp）不匹配，因此天然放行临时表。
var writeStatementPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\binsert\s+into\s+([a-z_][a-z0-9_]*)\s*\.`),
	regexp.MustCompile(`(?i)\bupdate\s+([a-z_][a-z0-9_]*)\s*\.`),
	regexp.MustCompile(`(?i)\bdelete\s+from\s+([a-z_][a-z0-9_]*)\s*\.`),
	regexp.MustCompile(`(?i)\bmerge\s+into\s+([a-z_][a-z0-9_]*)\s*\.`),
	regexp.MustCompile(`(?i)\btruncate\s+(?:table\s+)?([a-z_][a-z0-9_]*)\s*\.`),
	regexp.MustCompile(`(?i)\b(?:create|alter|drop)\s+(?:table|index|view|sequence|schema|function|type)\s+(?:if\s+(?:not\s+)?exists\s+)?([a-z_][a-z0-9_]*)\s*\.`),
	regexp.MustCompile(`(?i)\bcopy\s+([a-z_][a-z0-9_]*)\.[a-z_][a-z0-9_]*\s*(?:\([^)]*\))?\s*from\b`),
}

// TestNoCrossSchemaWrites 扫本仓全部 .go 的字符串常量：写语句指向非 risk schema 即红。
// 正则取语句里的 schema 名，因此 `public.audit_logs` 会以 `public` 命中——**不被豁免**。
func TestNoCrossSchemaWrites(t *testing.T) {
	root, err := repoRoot()
	if err != nil {
		t.Fatalf("locate repo root: %v", err)
	}

	var violations int
	walkErr := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == ".git" {
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		lits, err := sqlStringLiterals(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			rel = path
		}
		for _, lit := range lits {
			for _, schema := range foreignWritesIn(lit.text) {
				violations++
				t.Errorf("%s:%d writes to foreign schema %q: %s",
					rel, lit.line, schema, snippet(lit.text))
			}
		}
		return nil
	})
	if walkErr != nil {
		t.Fatalf("scan repo: %v", walkErr)
	}
	if violations > 0 {
		t.Fatalf("%d cross-schema write(s): only the risk schema may be written by this repo", violations)
	}
}

// TestForeignWriteGuardrail_Fixtures 是护栏自身的正反例。正例用拼接构造，
// 避免测试文件里的字面量被上面的全仓扫描命中（那会把护栏变成自指）。
func TestForeignWriteGuardrail_Fixtures(t *testing.T) {
	red := []string{
		"INSERT INTO " + "public" + ".audit_logs (id) VALUES ('x')",
		"UPDATE " + "quant" + ".runs SET status = 'x'",
		"DELETE FROM " + "market" + ".bars WHERE 1 = 1",
		"CREATE TABLE " + "broker" + ".orders (id TEXT)",
		"ALTER TABLE " + "strategy" + ".params ADD COLUMN x INT",
		"TRUNCATE " + "run" + ".runs",
		"COPY " + "analysis" + ".reports FROM STDIN",
	}
	for _, sql := range red {
		if got := foreignWritesIn(sql); len(got) == 0 {
			t.Errorf("guardrail missed foreign write: %s", sql)
		}
	}

	green := []string{
		"INSERT INTO " + "risk" + ".audit_logs (rule_id) VALUES ($1)",
		"UPDATE " + "risk" + ".rule_defs SET status = $1",
		"SELECT * FROM " + "public" + ".audit_logs",
		"SELECT * FROM " + "quant" + ".runs",
		"SELECT * FROM pg_catalog.pg_class",
		"SELECT * FROM information_schema.tables",
		"CREATE TEMP TABLE scratch (id INT)",
		"DELETE FROM " + "risk" + ".risk_events WHERE run_id = $1",
	}
	for _, sql := range green {
		if got := foreignWritesIn(sql); len(got) != 0 {
			t.Errorf("guardrail false positive on %q: %v", sql, got)
		}
	}
}

type sqlLiteral struct {
	line int
	text string
}

// sqlStringLiterals 用 Go AST 取一个文件里的全部字符串常量（含反引号原文），
// 只扫字符串、不扫注释，避免文档里提到的反例被误判。
func sqlStringLiterals(path string) ([]sqlLiteral, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		return nil, err
	}
	var lits []sqlLiteral
	ast.Inspect(file, func(n ast.Node) bool {
		bl, ok := n.(*ast.BasicLit)
		if !ok || bl.Kind != token.STRING {
			return true
		}
		text, err := strconv.Unquote(bl.Value)
		if err != nil {
			return true
		}
		lits = append(lits, sqlLiteral{line: fset.Position(bl.Pos()).Line, text: text})
		return true
	})
	return lits, nil
}

// foreignWritesIn 返回一条字符串常量里命中的禁止 schema（去重、稳定顺序）。
func foreignWritesIn(sql string) []string {
	seen := map[string]bool{}
	for _, pattern := range writeStatementPatterns {
		for _, match := range pattern.FindAllStringSubmatch(sql, -1) {
			schema := strings.ToLower(match[1])
			if foreignSchemaWriters[schema] {
				seen[schema] = true
			}
		}
	}
	if len(seen) == 0 {
		return nil
	}
	out := make([]string, 0, len(seen))
	for schema := range seen {
		out = append(out, schema)
	}
	sort.Strings(out)
	return out
}

func snippet(sql string) string {
	flat := strings.Join(strings.Fields(sql), " ")
	if len(flat) > 160 {
		return flat[:160] + "..."
	}
	return flat
}

// repoRoot 从测试包目录（internal/pgstore）向上找 go.mod。
func repoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", os.ErrNotExist
		}
		dir = parent
	}
}

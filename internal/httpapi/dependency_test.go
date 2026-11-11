package httpapi

import (
	"context"
	"database/sql/driver"
	"errors"
	"fmt"
	"net"
	"net/http"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"

	"tellurion-risk/internal/rule"
)

// TestIsDependencyUnavailable 钉住「只有连接级失败才是 503」的边界（RISK-PGDOWN-503 ④）：
// 依赖不可达要可判定、可重试；业务/数据错误不许被这条兜底吞成 503。
func TestIsDependencyUnavailable(t *testing.T) {
	connRefused := &net.OpError{Op: "dial", Net: "tcp", Err: errors.New("connect: connection refused")}
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"数据库连接已坏", fmt.Errorf("pgstore: begin: %w", driver.ErrBadConn), true},
		{"建连被拒（net.OpError）", connRefused, true},
		{"建连失败（pgconn.ConnectError）", &pgconn.ConnectError{Config: &pgconn.Config{}}, true},
		{"PG 连接异常类 08", &pgconn.PgError{Code: "08006"}, true},
		{"PG 停机中 57P03", &pgconn.PgError{Code: "57P03"}, true},
		{"业务错误：规则不存在", rule.ErrRuleNotFound, false},
		{"业务错误：唯一约束 23505", &pgconn.PgError{Code: "23505", ConstraintName: "rule_defs_pkey"}, false},
		{"数据错误：语法 42601", &pgconn.PgError{Code: "42601"}, false},
		{"普通内部错误", errors.New("boom"), false},
		{"请求超时/取消", context.DeadlineExceeded, false},
		{"nil", nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isDependencyUnavailable(tc.err); got != tc.want {
				t.Fatalf("isDependencyUnavailable(%T) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

// TestClassify_DependencyUnavailableIs503 钉住 classify 的接线：
// 依赖不可达 → 503 `dependency_unavailable`；内容寻址损坏等非依赖错误仍是 500。
func TestClassify_DependencyUnavailableIs503(t *testing.T) {
	status, code := classify(&net.OpError{Op: "read", Net: "tcp", Err: errors.New("connection reset")})
	if status != http.StatusServiceUnavailable || code != "dependency_unavailable" {
		t.Fatalf("classify(connection reset) = %d/%s, want 503/dependency_unavailable", status, code)
	}
	status, code = classify(rule.ErrSnapshotMismatch)
	if status != http.StatusInternalServerError || code != "internal" {
		t.Fatalf("classify(ErrSnapshotMismatch) = %d/%s, want 500/internal", status, code)
	}
}

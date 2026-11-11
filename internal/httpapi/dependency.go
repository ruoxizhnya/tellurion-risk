package httpapi

import (
	"database/sql/driver"
	"errors"
	"net"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"
)

// isDependencyUnavailable 判断 err 是否属于「后端依赖（PostgreSQL）不可达」。
//
// 只有连接级失败才归这一类；业务/数据错误（参数非法、唯一约束、快照内容损坏等）
// 仍走各自的 4xx / 500 映射，不允许被这条兜底吞成 503（RISK-PGDOWN-503 ④）。
//
// 认得的形态：
//   - pgx 建连失败（*pgconn.ConnectError，链上通常还带 *net.OpError）；
//   - 连接已坏（database/sql 的 driver.ErrBadConn，换连接重试后仍失败）；
//   - 网络层错误（*net.OpError，含 dial / read / write 失败）；
//   - PG 侧主动断开或拒连（SQLSTATE 08 类连接异常、57P01/57P02/57P03 停机中）。
//
// 这里读的是驱动错误的**形状**，不解析错误字符串：PG 不可达在运行时既可能是
// 建连拒绝（PG 停了），也可能是已建连接被对端掐断（PG 重启），两者都要映射成
// 「依赖不可用、可重试」，而同进程连接池会在 PG 恢复后自愈。
func isDependencyUnavailable(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, driver.ErrBadConn) {
		return true
	}
	var connectErr *pgconn.ConnectError
	if errors.As(err, &connectErr) {
		return true
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		if strings.HasPrefix(pgErr.Code, "08") {
			return true
		}
		switch pgErr.Code {
		case "57P01", "57P02", "57P03":
			return true
		}
	}
	var opErr *net.OpError
	return errors.As(err, &opErr)
}

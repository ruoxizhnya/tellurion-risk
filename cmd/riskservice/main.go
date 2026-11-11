// Command riskservice 是风控规则真相源的进程入口。
//
// 存储：-dsn / RISK_DATABASE_URL 非空时用 PostgreSQL（启动时执行幂等 DDL），
// 否则退回进程内 Memory Store（仅本地开发；重启即丢，不得用于生产）。
package main

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib" // 注册 "pgx" database/sql 驱动
	"github.com/ruoxizhnya/tellurion-kit/auth"
	kithttp "github.com/ruoxizhnya/tellurion-kit/auth/httpadapter"

	"tellurion-risk/internal/breaker"
	"tellurion-risk/internal/event"
	"tellurion-risk/internal/httpapi"
	"tellurion-risk/internal/pgstore"
	"tellurion-risk/internal/rule"
)

// defaultAddr 是默认监听地址：回环 + 8089（ADR-036 G0）。
//
// 8089 而不是 8086：8086 是 ai-service 的口径，同机联调会撞号。回环是默认值，
// 非回环绑定必须显式给 -addr 并过启动闸（无 secret + 非回环 = 拒绝启动）。
const defaultAddr = "127.0.0.1:8089"

// defaultShutdownTimeout 是有界排空的默认上限（RISK_SHUTDOWN_TIMEOUT 可调）。
// 停机语义：先关监听（不再接新连接），再等在途 handler 跑完；超时则明确报错、
// 以非 0 退出，不静默把在途请求丢掉。
const defaultShutdownTimeout = 10 * time.Second

func main() {
	addr := flag.String("addr", defaultAddr, "listen address")
	dsn := flag.String("dsn", os.Getenv("RISK_DATABASE_URL"), "PostgreSQL DSN (empty = in-memory store)")
	jwtSecret := flag.String("jwt-secret", envFirst("JWT_SECRET", "AUTH_JWT_SECRET"), "JWT secret shared with analysis (empty = open access on loopback only)")
	exposure := flag.String("insecure-exposure", os.Getenv("AUTH_INSECURE_EXPOSURE"),
		"explicit exposure declaration for open access on a non-loopback bind (only loopback-published is accepted)")
	flag.Parse()

	// 启动闸（裁-15）：有 secret ⇒ 鉴权；无 secret + 回环 ⇒ 开放；无 secret + 非回环 ⇒ 拒绝启动。
	startup := kithttp.DecideStartup(*jwtSecret, true, bindHost(*addr), *exposure)
	if startup.Refuse {
		log.Fatalf("riskservice: %s", startup.Reason)
	}
	if startup.Insecure {
		log.Printf("riskservice: WARNING authentication disabled (open access); bind address %q is loopback-only", *addr)
	}

	stores, closeStores, err := openStores(*dsn)
	if err != nil {
		log.Fatalf("riskservice: %v", err)
	}
	defer closeStores()

	svc, err := rule.New(rule.Options{Store: stores.rules})
	if err != nil {
		log.Fatalf("riskservice: %v", err)
	}
	events, err := event.New(event.Options{Store: stores.events})
	if err != nil {
		log.Fatalf("riskservice: %v", err)
	}
	breakers, err := breaker.New(breaker.Options{Store: stores.breakers})
	if err != nil {
		log.Fatalf("riskservice: %v", err)
	}
	tokens := auth.NewTokenService(auth.Config{JWTSecret: startup.Secret})
	// G4：monitor 的 `configured` 判别位是部署声明，不是探测——本仓看不见 core 的
	// `risk_service.events_enabled`。默认 false 与 core 出厂值对齐；声明与 core
	// 实际开关不一致属于部署错误，核对见 docs/runbook.md §5。
	eventsExpected := envBool("RISK_EVENTS_EXPECTED")
	srv := &http.Server{
		Addr: *addr,
		Handler: httpapi.New(svc,
			httpapi.WithEvents(events),
			httpapi.WithBreakers(breakers),
			httpapi.WithAuthorizer(httpapi.NewAuthorizer(tokens)),
			httpapi.WithReadiness(stores.ready),
			httpapi.WithEventIngestConfigured(eventsExpected),
		).Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}
	log.Printf("riskservice: event ingest declared=%t (RISK_EVENTS_EXPECTED)", eventsExpected)

	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		log.Fatalf("riskservice: listen %s: %v", *addr, err)
	}
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGTERM, os.Interrupt)
	defer signal.Stop(stop)

	log.Printf("riskservice: listening on %s (auth=%t)", ln.Addr(), tokens.Enabled())
	if err := serve(srv, ln, stop); err != nil {
		log.Fatalf("riskservice: %v", err)
	}
	log.Printf("riskservice: drained, exiting")
}

// serve 在 ln 上跑 HTTP，直到收到停机信号或服务出错。
//
// 收到信号后只做两件事：关闭监听（拒绝新连接）等在途 handler 自然结束；
// 超过有界超时仍未排空 ⇒ 返回错误（进程非 0 退出），不假装优雅。
// handler 的 request context 不因停机被取消，因此在途事务按正常路径 COMMIT/ROLLBACK，
// 不会留下半完成态（ADR-0003 的事务边界）。
func serve(srv *http.Server, ln net.Listener, stop <-chan os.Signal) error {
	serveErr := make(chan error, 1)
	go func() {
		err := srv.Serve(ln)
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
		serveErr <- err
	}()

	select {
	case err := <-serveErr:
		return err
	case sig := <-stop:
		log.Printf("riskservice: received %v, draining in-flight requests (timeout %s)",
			sig, shutdownTimeout())
	}

	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout())
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Printf("riskservice: drain did not finish within %s: %v", shutdownTimeout(), err)
		return fmt.Errorf("graceful shutdown: %w", err)
	}
	return <-serveErr
}

// shutdownTimeout 读 RISK_SHUTDOWN_TIMEOUT（缺省 10s）；非法或非正值回退缺省并告警。
func shutdownTimeout() time.Duration {
	raw := strings.TrimSpace(os.Getenv("RISK_SHUTDOWN_TIMEOUT"))
	if raw == "" {
		return defaultShutdownTimeout
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d <= 0 {
		log.Printf("riskservice: ignoring invalid RISK_SHUTDOWN_TIMEOUT=%q, using %v", raw, defaultShutdownTimeout)
		return defaultShutdownTimeout
	}
	return d
}

// bindHost 从 -addr 取绑定主机；取不到端口时原样当主机比（空 = 全网卡）。
func bindHost(addr string) string {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return addr
	}
	return host
}

// envFirst 按顺序取第一个非空环境变量（core 历史用法 JWT_SECRET、配置口径 AUTH_JWT_SECRET）。
func envFirst(names ...string) string {
	for _, name := range names {
		if v := strings.TrimSpace(os.Getenv(name)); v != "" {
			return v
		}
	}
	return ""
}

// envBool 读布尔开关；缺省与非法值一律 false（非法值告警，不猜）。
func envBool(name string) bool {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return false
	}
	v, err := strconv.ParseBool(raw)
	if err != nil {
		log.Printf("riskservice: ignoring invalid %s=%q (want true/false)", name, raw)
		return false
	}
	return v
}

// stores 汇总本进程装配的各个 Store；PG 模式下同一个 *pgstore.Store 满足全部端口。
type stores struct {
	rules    rule.Store
	events   event.Store
	breakers breaker.Store
	// ready 是 /readyz 的检查（G1）：PG 可连 + 迁移已执行；内存档恒就绪。
	ready func(context.Context) error
}

// openStores 选择持久化实现并跑迁移。空 DSN = Memory（显式告警，避免误上生产）。
func openStores(dsn string) (stores, func(), error) {
	if dsn == "" {
		log.Printf("riskservice: RISK_DATABASE_URL is empty, using in-memory store (non-persistent)")
		return stores{
			rules:    rule.NewMemory(),
			events:   event.NewMemory(),
			breakers: breaker.NewMemory(),
			ready:    func(context.Context) error { return nil },
		}, func() {}, nil
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return stores{}, nil, err
	}
	// 启动不可无界阻塞（G1）：连接 + 迁移共用一个超时，可用 RISK_DB_TIMEOUT 调节。
	timeout := 15 * time.Second
	if raw := os.Getenv("RISK_DB_TIMEOUT"); raw != "" {
		if d, err := time.ParseDuration(raw); err == nil && d > 0 {
			timeout = d
		} else {
			log.Printf("riskservice: ignoring invalid RISK_DB_TIMEOUT=%q, using %v", raw, timeout)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return stores{}, nil, err
	}
	if err := pgstore.Migrate(ctx, db); err != nil {
		_ = db.Close()
		return stores{}, nil, err
	}
	log.Printf("riskservice: using PostgreSQL store, migrations applied")
	st := pgstore.New(db)
	ready := func(ctx context.Context) error {
		if err := db.PingContext(ctx); err != nil {
			return fmt.Errorf("postgres unreachable: %w", err)
		}
		var migrated bool
		if err := db.QueryRowContext(ctx, `SELECT to_regclass('risk.rule_defs') IS NOT NULL`).Scan(&migrated); err != nil {
			return fmt.Errorf("readiness query: %w", err)
		}
		if !migrated {
			return errors.New("migrations not applied")
		}
		return nil
	}
	return stores{rules: st, events: st, breakers: st, ready: ready}, func() { _ = db.Close() }, nil
}

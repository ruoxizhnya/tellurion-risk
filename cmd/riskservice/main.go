// Command riskservice 是风控规则真相源的进程入口。
//
// 存储：-dsn / RISK_DATABASE_URL 非空时用 PostgreSQL（启动时执行幂等 DDL），
// 否则退回进程内 Memory Store（仅本地开发；重启即丢，不得用于生产）。
package main

import (
	"context"
	"database/sql"
	"flag"
	"log"
	"net/http"
	"os"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib" // 注册 "pgx" database/sql 驱动

	"tellurion-risk/internal/breaker"
	"tellurion-risk/internal/event"
	"tellurion-risk/internal/httpapi"
	"tellurion-risk/internal/pgstore"
	"tellurion-risk/internal/rule"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:8086", "listen address")
	dsn := flag.String("dsn", os.Getenv("RISK_DATABASE_URL"), "PostgreSQL DSN (empty = in-memory store)")
	flag.Parse()

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
	srv := &http.Server{
		Addr: *addr,
		Handler: httpapi.New(svc,
			httpapi.WithEvents(events),
			httpapi.WithBreakers(breakers),
		).Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}
	log.Printf("riskservice: listening on %s", *addr)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("riskservice: %v", err)
	}
}

// stores 汇总本进程装配的各个 Store；PG 模式下同一个 *pgstore.Store 满足全部端口。
type stores struct {
	rules    rule.Store
	events   event.Store
	breakers breaker.Store
}

// openStores 选择持久化实现并跑迁移。空 DSN = Memory（显式告警，避免误上生产）。
func openStores(dsn string) (stores, func(), error) {
	if dsn == "" {
		log.Printf("riskservice: RISK_DATABASE_URL is empty, using in-memory store (non-persistent)")
		return stores{
			rules:    rule.NewMemory(),
			events:   event.NewMemory(),
			breakers: breaker.NewMemory(),
		}, func() {}, nil
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return stores{}, nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
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
	return stores{rules: st, events: st, breakers: st}, func() { _ = db.Close() }, nil
}

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

	"tellurion-risk/internal/httpapi"
	"tellurion-risk/internal/pgstore"
	"tellurion-risk/internal/rule"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:8086", "listen address")
	dsn := flag.String("dsn", os.Getenv("RISK_DATABASE_URL"), "PostgreSQL DSN (empty = in-memory store)")
	flag.Parse()

	store, closeStore, err := openStore(*dsn)
	if err != nil {
		log.Fatalf("riskservice: %v", err)
	}
	defer closeStore()

	svc, err := rule.New(rule.Options{Store: store})
	if err != nil {
		log.Fatalf("riskservice: %v", err)
	}
	srv := &http.Server{
		Addr:              *addr,
		Handler:           httpapi.New(svc).Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}
	log.Printf("riskservice: listening on %s", *addr)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("riskservice: %v", err)
	}
}

// openStore 选择持久化实现并跑迁移。空 DSN = Memory（显式告警，避免误上生产）。
func openStore(dsn string) (rule.Store, func(), error) {
	if dsn == "" {
		log.Printf("riskservice: RISK_DATABASE_URL is empty, using in-memory store (non-persistent)")
		return rule.NewMemory(), func() {}, nil
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, nil, err
	}
	if err := pgstore.Migrate(ctx, db); err != nil {
		_ = db.Close()
		return nil, nil, err
	}
	log.Printf("riskservice: using PostgreSQL store, migrations applied")
	return pgstore.New(db), func() { _ = db.Close() }, nil
}

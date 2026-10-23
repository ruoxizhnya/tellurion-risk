// Command riskservice 是风控规则真相源的进程入口。
//
// 当前只装配规则侧（定义 / 版本 / 审批 / 审计）在 Memory Store 上；
// PG Store、迁移执行入口、快照下发、事件回传、熔断、监控均未接（docs/TASKS.md QT-42 施工记录）。
package main

import (
	"flag"
	"log"
	"net/http"
	"time"

	"tellurion-risk/internal/httpapi"
	"tellurion-risk/internal/rule"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:8086", "listen address")
	flag.Parse()

	svc, err := rule.New(rule.Options{Store: rule.NewMemory()})
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

package main

import (
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	kithttp "github.com/ruoxizhnya/tellurion-kit/auth/httpadapter"

	"tellurion-risk/internal/httpapi"
	"tellurion-risk/internal/rule"
)

// TestExplicitExposureAllowsNonLoopbackBind 钉住第 3 态的绑定口径：容器内一律绑
// 0.0.0.0，显式声明 loopback-published 后**不因非回环拒绝启动**，且写面走业务
// （POST /api/risk/rules = 201，不是 503）。
func TestExplicitExposureAllowsNonLoopbackBind(t *testing.T) {
	startup := kithttp.DecideStartup("", true, "0.0.0.0", "loopback-published")
	if startup.Refuse {
		t.Fatalf("explicit exposure must allow non-loopback bind, refused: %s", startup.Reason)
	}
	if !openAccessEnabled(startup, "loopback-published") {
		t.Fatal("explicit exposure must enable open access")
	}

	svc, err := rule.New(rule.Options{Store: rule.NewMemory()})
	if err != nil {
		t.Fatalf("rule.New: %v", err)
	}
	srv := httptest.NewServer(httpapi.New(svc,
		httpapi.WithAuthorizer(httpapi.NewOpenAccessAuthorizer())).Handler())
	defer srv.Close()

	const body = `{"rule_id":"r-nonloop","scope":"strategy","type":"stop_loss","params":` +
		`{"atr_period":14,"base_multiplier":2.5,"bear_multiplier":3.5,"bull_multiplier":2.0,"sideways_multiplier":2.5}}`
	resp, err := http.Post(srv.URL+"/api/risk/rules", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST /api/risk/rules: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("non-loopback open access create = %d, want 201: %s", resp.StatusCode, raw)
	}
}

// TestOpenAccessRequiresExplicitExposure 钉住第 3 态的判定：只有启动闸已允许
// 「无 secret 启动」**且**逐字声明 loopback-published 才放开写面。缺声明 = 第 2 态
// （写端点 503），拼错/大小写不同也不猜。
func TestOpenAccessRequiresExplicitExposure(t *testing.T) {
	insecure := kithttp.Startup{Insecure: true}
	cases := []struct {
		name     string
		startup  kithttp.Startup
		exposure string
		want     bool
	}{
		{"no declaration is state 2", insecure, "", false},
		{"misspelled declaration refuses to guess", insecure, "loopback", false},
		{"uppercase is not accepted", insecure, "LOOPBACK-PUBLISHED", false},
		{"explicit declaration is state 3", insecure, " loopback-published ", true},
		{"secret present never opens", kithttp.Startup{Secret: []byte("s")}, "loopback-published", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := openAccessEnabled(tc.startup, tc.exposure); got != tc.want {
				t.Fatalf("openAccessEnabled(%+v, %q) = %t, want %t", tc.startup, tc.exposure, got, tc.want)
			}
		})
	}
}

// TestDefaultAddr 钉住 ADR-036 G0 的默认监听端口。
//
// 8086 是 ai-service 的口径，本仓回到 8086 就会和同机联调撞号；这条测试是
// `deploy/compose.yaml`（8089）与代码默认值之间唯一的机械对拍点。
func TestDefaultAddr(t *testing.T) {
	if defaultAddr != "127.0.0.1:8089" {
		t.Fatalf("default listen address = %q, want 127.0.0.1:8089 (ADR-036 G0)", defaultAddr)
	}
}

// TestServeDrainsInFlightRequests 是停机语义的机械判据（RISK-GRACEFUL-SHUTDOWN）：
//
//  1. 收到停机信号后 serve 不得立刻返回，必须等在途请求跑完；
//  2. 监听必须先关（新连接被拒），不能接住新请求再硬断；
//  3. 在途请求必须拿到正常 200，而不是连接被硬断；
//  4. 排空完成后 serve 返回 nil（进程退出码 0）。
//
// 破坏验证：删掉 serve 里的 srv.Shutdown 调用，第 1 条会先红。
func TestServeDrainsInFlightRequests(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		once.Do(func() { close(started) })
		<-release
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "done")
	})}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := ln.Addr().String()

	stop := make(chan os.Signal, 1)
	done := make(chan error, 1)
	go func() { done <- serve(srv, ln, stop) }()

	type result struct {
		status int
		body   string
		err    error
	}
	respCh := make(chan result, 1)
	go func() {
		resp, err := http.Get("http://" + addr + "/")
		if err != nil {
			respCh <- result{err: err}
			return
		}
		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		respCh <- result{status: resp.StatusCode, body: string(body), err: err}
	}()

	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("在途请求没有开始，用例前提不成立")
	}

	stop <- syscall.SIGTERM

	// 在途请求未结束前 serve 不得返回（否则就是硬断）。
	select {
	case err := <-done:
		t.Fatalf("serve 在在途请求完成前就返回（%v），属于硬断而非排空", err)
	case <-time.After(200 * time.Millisecond):
	}

	// Shutdown 先关监听：停机中不得再接受新连接。
	if c, err := net.DialTimeout("tcp", addr, time.Second); err == nil {
		_ = c.Close()
		t.Fatal("停机中监听仍在接受新连接")
	}

	close(release)

	select {
	case got := <-respCh:
		if got.err != nil {
			t.Fatalf("在途请求被硬断：%v", got.err)
		}
		if got.status != http.StatusOK || got.body != "done" {
			t.Fatalf("在途请求 status=%d body=%q, want 200 %q", got.status, got.body, "done")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("在途请求没有完成")
	}

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("排空完成后 serve 应返回 nil（退出码 0），got %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("排空完成后 serve 没有返回")
	}
}

// TestServeShutdownTimeoutIsLoud 钉住「有界」：排空超时必须返回错误（非 0 退出），
// 不能无限等，也不能静默把在途请求当作已完成。
func TestServeShutdownTimeoutIsLoud(t *testing.T) {
	t.Setenv("RISK_SHUTDOWN_TIMEOUT", "50ms")
	started := make(chan struct{})
	release := make(chan struct{})
	defer close(release)
	var once sync.Once
	srv := &http.Server{Handler: http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		once.Do(func() { close(started) })
		<-release
	})}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := ln.Addr().String()

	stop := make(chan os.Signal, 1)
	done := make(chan error, 1)
	go func() { done <- serve(srv, ln, stop) }()

	go func() {
		resp, err := http.Get("http://" + addr + "/")
		if err == nil {
			_ = resp.Body.Close()
		}
	}()

	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("在途请求没有开始，用例前提不成立")
	}

	stop <- syscall.SIGTERM

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("排空超时后 serve 应返回错误（非 0 退出），而不是报告优雅完成")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("有界排空失效：超时后 serve 仍未返回")
	}
}

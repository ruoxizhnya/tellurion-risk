package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"tellurion-risk/internal/event"
	"tellurion-risk/internal/rule"
)

// configuredOf 读取 monitor 响应里的 G4 判别位；ok=false 表示字段缺失
// （缺失本身就是要防的「两种空集混为一谈」）。
func configuredOf(t *testing.T, body string) (value, ok bool) {
	t.Helper()
	var fields map[string]json.RawMessage
	if err := json.Unmarshal([]byte(body), &fields); err != nil {
		t.Fatalf("decode %q: %v", body, err)
	}
	raw, ok := fields["configured"]
	if !ok {
		return false, false
	}
	if err := json.Unmarshal(raw, &value); err != nil {
		t.Fatalf("configured = %s: %v", raw, err)
	}
	return value, true
}

// ③ 空集的两义必须可判别：同一个窗口、同样是零条记录，
// 未接入 ⇒ configured:false；已接入但没触发 ⇒ configured:true。
func TestMonitor_EmptySetIsDiscriminable(t *testing.T) {
	endpoints := []struct{ method, path, body string }{
		{http.MethodGet, "/api/risk/monitor/exposure", ""},
		{http.MethodGet, "/api/risk/monitor/alerts", ""},
		{http.MethodPost, "/api/risk/monitor/evaluate", `{}`},
	}
	for _, tc := range []struct {
		name       string
		configured bool
	}{
		{"未接入", false},
		{"已接入但没触发", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newMonitorHarness(t,
				WithAuthorizer(authFunc(allowAll)),
				WithEventIngestConfigured(tc.configured),
			)
			for _, ep := range endpoints {
				resp, body := h.do(t, ep.method, ep.path, ep.body)
				if resp.StatusCode != http.StatusOK {
					t.Fatalf("%s: status %d body %s", ep.path, resp.StatusCode, body)
				}
				got, ok := configuredOf(t, body)
				if !ok {
					t.Fatalf("%s: 响应缺少 %q 判别位：%s", ep.path, "configured", body)
				}
				if got != tc.configured {
					t.Fatalf("%s: configured = %v, want %v（body %s）", ep.path, got, tc.configured, body)
				}
			}
		})
	}
}

// errReader 模拟运行期 PG 不可达：读端口返回连接错误。
// 语义要求（internal/event/reader.go）：读不到数据必须报错，不许拿空集冒充「没有敞口」。
type errReader struct{ err error }

func (r errReader) QueryEvents(context.Context, event.Query) ([]event.Record, bool, error) {
	return nil, false, r.err
}

// 失败语义表第 3 类：规则库为空是「已知的空」（200 + `[]`），不是 500、不是 null；
// 但 resolve 必须 404 —— 空库不许被读成「有规则、只是没触发」。
func TestAPI_EmptyRuleLibraryIsExplicit(t *testing.T) {
	h := newHarness(t)
	resp, body := h.do(t, http.MethodGet, "/api/risk/rules", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /rules: status %d body %s", resp.StatusCode, body)
	}
	var list struct {
		Rules []json.RawMessage `json:"rules"`
		Count int               `json:"count"`
	}
	if err := json.Unmarshal([]byte(body), &list); err != nil {
		t.Fatalf("decode %q: %v", body, err)
	}
	if list.Rules == nil {
		t.Fatalf("空库必须回 [] 而不是 null：%s", body)
	}
	if list.Count != 0 {
		t.Fatalf("空库 count = %d, want 0", list.Count)
	}
	resp, body = h.do(t, http.MethodPost, "/api/risk/resolve", `{"ruleset":"latest"}`)
	wantCode(t, resp.StatusCode, body, http.StatusNotFound, "no_approved_rules")
}

// 失败语义表第 2 类：PG 不可达 ⇒ 非 2xx，且是可判定的 503 `dependency_unavailable`
// （不是「空集 200」，也不是把「重启就能好」报成服务端事故 500）。
// 同时证明 monitor 的 configured 判别位不会把「读失败」伪装成 false。
func TestMonitor_PGUnreachableIsNotEmptySet(t *testing.T) {
	now := func() time.Time { return time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC) }
	ruleSvc, err := rule.New(rule.Options{Store: rule.NewMemory(), Now: now})
	if err != nil {
		t.Fatalf("rule.New: %v", err)
	}
	events, err := event.New(event.Options{
		Store: event.NewMemory(),
		Reader: errReader{err: &net.OpError{
			Op:  "dial",
			Net: "tcp",
			Err: errors.New("connect: connection refused"),
		}},
		Now: now,
	})
	if err != nil {
		t.Fatalf("event.New: %v", err)
	}
	srv := New(ruleSvc, WithEvents(events), WithAuthorizer(authFunc(allowAll)), WithEventIngestConfigured(true))

	for _, path := range []string{"/api/risk/monitor/exposure", "/api/risk/monitor/alerts"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, req)
		wantCode(t, rec.Code, rec.Body.String(), http.StatusServiceUnavailable, "dependency_unavailable")
	}
}

// ⑤ 演练（本仓侧）：停服后调用方（core）拿到的必须是连接错误，
// 不是 2xx，也不能有「回落默认限额」这类静默降级。
// 覆盖 core 的两条腿：risk_rules_hash（POST /resolve）与告警 relay（POST /events）。
func TestDrill_StoppedServiceFailsLoud(t *testing.T) {
	h := newHarness(t)
	ts := httptest.NewServer(h.srv.Handler())
	defer ts.Close()
	client := &http.Client{Timeout: 5 * time.Second}

	post := func(path, body string) (*http.Response, string, error) {
		req, err := http.NewRequest(http.MethodPost, ts.URL+path, strings.NewReader(body))
		if err != nil {
			return nil, "", err
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := client.Do(req)
		if err != nil {
			return nil, "", err
		}
		defer resp.Body.Close()
		raw, err := io.ReadAll(resp.Body)
		return resp, string(raw), err
	}

	// 起：先证明两条腿都活着（否则「停了会报错」没有意义）。
	if resp, body, err := post("/api/risk/rules", createBody); err != nil || resp.StatusCode != http.StatusCreated {
		t.Fatalf("create before stop: status %v err %v body %s", resp, err, body)
	}
	if resp, body, err := post("/api/risk/rules/r-sl/approve", `{"revision":1}`); err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("approve before stop: status %v err %v body %s", resp, err, body)
	}
	resp, body, err := post("/api/risk/resolve", `{"ruleset":"latest"}`)
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("resolve before stop: status %v err %v body %s", resp, err, body)
	}
	if got := decode[resolveWire](t, body); len(got.RiskRulesHash) != 64 {
		t.Fatalf("resolve before stop must return a 64-hex hash, got %q", got.RiskRulesHash)
	}
	events := `{"events":[{"run_id":"run-1","order_id":"o-1","event_seq":1,"symbol":"600000.SH","verdict":"allowed","ts":"2026-10-23T11:00:00Z","payload":{"side":"buy","qty":1,"price":10}}]}`
	if resp, body, err := post("/api/risk/events", events); err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("events before stop: status %v err %v body %s", resp, err, body)
	}

	// 停：关监听（等价于本仓进程不在了）。
	client.CloseIdleConnections()
	ts.Close()

	for _, leg := range []struct{ name, path, body string }{
		{"risk_rules_hash 腿", "/api/risk/resolve", `{"ruleset":"latest"}`},
		{"告警 relay 腿", "/api/risk/events", events},
	} {
		resp, _, err := post(leg.path, leg.body)
		if err == nil {
			t.Fatalf("%s: 停服后仍拿到响应（status %d），属于静默降级", leg.name, resp.StatusCode)
		}
		t.Logf("%s: 停服后连接失败（响亮）：%v", leg.name, err)
	}
}

package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ruoxizhnya/tellurion-kit/auth"

	"tellurion-risk/internal/event"
	"tellurion-risk/internal/rule"
)

// captureStore 记录落库行，供断言 actor 用。
type captureStore struct {
	mu      sync.Mutex
	records []event.Record
}

func (c *captureStore) PutEvents(_ context.Context, recs []event.Record) (int, int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.records = append(c.records, recs...)
	return len(recs), 0, nil
}

func (c *captureStore) last(t *testing.T) event.Record {
	t.Helper()
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.records) == 0 {
		t.Fatal("no event recorded")
	}
	return c.records[len(c.records)-1]
}

// QT-80：actor 来自鉴权 claims，不由请求体自报；open-access 取显式常量。
func TestEvents_RECORDActorFromClaimsNotBody(t *testing.T) {
	const body = `{"events":[{"run_id":"run-actor","order_id":"ord-1","event_seq":1,"ts":"2026-11-07T01:00:00Z","verdict":"allow","actor":"forged"}]}`

	cases := []struct {
		name       string
		authorizer Authorizer // nil = open-access（不装鉴权闸）
		want       string
	}{
		{
			name: "service-token-sub",
			authorizer: authFunc(func(*http.Request) (Principal, error) {
				return Principal{Subject: "service:core", Role: "service"}, nil
			}),
			want: "service:core",
		},
		{
			name: "user-username",
			authorizer: authFunc(func(*http.Request) (Principal, error) {
				return Principal{Subject: "u-1", Username: "alice", Role: "admin"}, nil
			}),
			want: "alice",
		},
		{
			name:       "open-access",
			authorizer: NewAuthorizer(auth.NewTokenService(auth.Config{})),
			want:       ActorOpenAccess,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := &captureStore{}
			events, err := event.New(event.Options{
				Store: store,
				Now:   func() time.Time { return time.Date(2026, 11, 7, 1, 0, 0, 0, time.UTC) },
			})
			if err != nil {
				t.Fatalf("event.New: %v", err)
			}
			svc, err := rule.New(rule.Options{Store: rule.NewMemory()})
			if err != nil {
				t.Fatalf("rule.New: %v", err)
			}
			srv := New(svc, WithEvents(events), WithAuthorizer(tc.authorizer))

			req := httptest.NewRequest(http.MethodPost, "/api/risk/events", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			srv.Handler().ServeHTTP(rec, req)
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
			}
			if got := store.last(t).Actor; got != tc.want {
				t.Fatalf("actor = %q, want %q (must come from claims, not the body)", got, tc.want)
			}
		})
	}
}

package breaker

import (
	"context"
	"errors"
	"testing"
	"time"

	"tellurion-risk/internal/rule"
)

func newService(t *testing.T) *Service {
	t.Helper()
	svc, err := New(Options{
		Store: NewMemory(),
		Now:   func() time.Time { return time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return svc
}

func command(id string) Command {
	return Command{
		BreakerID: id,
		Action:    ActionHalt,
		Scope:     rule.ScopeGlobal,
		Reason:    "daily drawdown breach",
		Ts:        time.Date(2026, 10, 24, 11, 59, 0, 0, time.UTC),
	}
}

func TestApply_IdempotencyKey(t *testing.T) {
	svc := newService(t)
	ctx := context.Background()
	cmd := command("b-1")

	created, err := svc.Apply(ctx, cmd)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if !created {
		t.Fatal("first apply must be created")
	}

	// 同键同载荷重放（客户端重新生成 ts）→ already_applied，不报冲突。
	replay := cmd
	replay.Ts = cmd.Ts.Add(time.Minute)
	created, err = svc.Apply(ctx, replay)
	if err != nil {
		t.Fatalf("replay Apply: %v", err)
	}
	if created {
		t.Fatal("replay must be already_applied")
	}

	// 同键不同载荷 → 409 语义的领域哨兵。
	diff := cmd
	diff.Action = ActionCancelAll
	if _, err := svc.Apply(ctx, diff); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("different payload: want ErrIdempotencyConflict, got %v", err)
	}
	diff = cmd
	diff.Reason = "other"
	if _, err := svc.Apply(ctx, diff); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("different reason: want ErrIdempotencyConflict, got %v", err)
	}
}

func TestValidate_FailsClosed(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*Command)
		want   error
	}{
		{"breaker_id missing", func(c *Command) { c.BreakerID = "" }, ErrInvalidParams},
		{"breaker_id blank", func(c *Command) { c.BreakerID = "  " }, ErrInvalidParams},
		{"unknown action", func(c *Command) { c.Action = "liquidate" }, ErrInvalidAction},
		{"unknown scope", func(c *Command) { c.Scope = "planet" }, ErrInvalidScope},
		{"ts zero", func(c *Command) { c.Ts = time.Time{} }, ErrInvalidParams},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cmd := command("b-1")
			tc.mutate(&cmd)
			if _, err := newService(t).Apply(context.Background(), cmd); !errors.Is(err, tc.want) {
				t.Fatalf("want %v, got %v", tc.want, err)
			}
		})
	}
}

func TestCommand_SamePayloadIgnoresTimeAndID(t *testing.T) {
	a := command("b-1")
	b := command("b-1")
	b.Ts = a.Ts.Add(time.Hour)
	if !a.SamePayload(b) {
		t.Fatal("ts must not participate in payload identity")
	}
	b.Reason = "different"
	if a.SamePayload(b) {
		t.Fatal("reason must participate in payload identity")
	}
}

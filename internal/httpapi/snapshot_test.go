package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"tellurion-risk/internal/rule"
)

type snapshotRuleWire struct {
	RuleID      string          `json:"rule_id"`
	Revision    int             `json:"revision"`
	Scope       string          `json:"scope"`
	Type        string          `json:"type"`
	Params      json.RawMessage `json:"params"`
	VersionHash string          `json:"version_hash"`
}

type snapshotWire struct {
	RiskRulesHash string             `json:"risk_rules_hash"`
	Rules         []snapshotRuleWire `json:"rules"`
	CreatedAt     time.Time          `json:"created_at"`
}

type resolveWire struct {
	RiskRulesHash string       `json:"risk_rules_hash"`
	Snapshot      snapshotWire `json:"snapshot"`
}

func createAndApprove(t *testing.T, h *harness, ruleID, params string) {
	t.Helper()
	resp, body := h.do(t, http.MethodPost, "/api/risk/rules",
		`{"rule_id":"`+ruleID+`","scope":"strategy","type":"stop_loss","params":`+params+`}`)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create %s: status %d body %s", ruleID, resp.StatusCode, body)
	}
	resp, body = h.do(t, http.MethodPost, "/api/risk/rules/"+ruleID+"/approve", `{"revision":1}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("approve %s: status %d body %s", ruleID, resp.StatusCode, body)
	}
}

func TestAPI_ResolveLatestAndSnapshotByteIdentity(t *testing.T) {
	h := newHarness(t)
	createAndApprove(t, h, "r-sl", `{"pct":2.5}`)
	createAndApprove(t, h, "r-other", `{"pct":7.5}`)

	resp, body := h.do(t, http.MethodPost, "/api/risk/resolve", `{"ruleset":"latest"}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("resolve: status %d body %s", resp.StatusCode, body)
	}
	resolved := decode[resolveWire](t, body)
	if len(resolved.RiskRulesHash) != 64 || resolved.RiskRulesHash != resolved.Snapshot.RiskRulesHash {
		t.Fatalf("resolve body: %+v", resolved)
	}
	if len(resolved.Snapshot.Rules) != 2 {
		t.Fatalf("snapshot must carry 2 rules, got %d", len(resolved.Snapshot.Rules))
	}
	if resolved.Snapshot.CreatedAt.IsZero() {
		t.Fatalf("snapshot must carry created_at")
	}
	hash := resolved.RiskRulesHash

	// 不变量 2（docs/dataflow.md §4）：同一 hash 两次拉取字节一致。
	_, body1 := h.do(t, http.MethodGet, "/api/risk/snapshots/"+hash, "")
	_, body2 := h.do(t, http.MethodGet, "/api/risk/snapshots/"+hash, "")
	if body1 == "" || body1 != body2 {
		t.Fatalf("same hash must return byte-identical bodies:\n a=%s\n b=%s", body1, body2)
	}
	snap := decode[snapshotWire](t, body1)
	if snap.RiskRulesHash != hash || len(snap.Rules) != 2 {
		t.Fatalf("snapshot body: %+v", snap)
	}

	// bundle 与快照本体同一形状：ParseBundle 直接吃。
	_, bundleBody := h.do(t, http.MethodGet, "/api/risk/snapshots/"+hash+"/bundle", "")
	if bundleBody != body1 {
		t.Fatalf("bundle must be the canonical snapshot JSON:\n snapshot=%s\n bundle=%s", body1, bundleBody)
	}
	b, err := rule.ParseBundle([]byte(bundleBody))
	if err != nil {
		t.Fatalf("ParseBundle over wire body: %v", err)
	}
	if b.RiskRulesHash != hash || len(b.ForScope("strategy")) != 2 {
		t.Fatalf("bundle content: hash %q, strategy rules %d", b.RiskRulesHash, len(b.ForScope("strategy")))
	}

	// draft 永不进快照：出新 draft 修订后再 resolve，快照仍指 rev1。
	updResp, updBody := h.do(t, http.MethodPut, "/api/risk/rules/r-sl", `{"base_revision":1,"params":{"pct":5}}`)
	if updResp.StatusCode != http.StatusOK {
		t.Fatalf("update: status %d body %s", updResp.StatusCode, updBody)
	}
	resp, body = h.do(t, http.MethodPost, "/api/risk/resolve", `{"ruleset":"latest"}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("re-resolve: status %d body %s", resp.StatusCode, body)
	}
	again := decode[resolveWire](t, body)
	if again.RiskRulesHash != hash || again.Snapshot.Rules[0].Revision != 1 {
		t.Fatalf("draft revision must not enter snapshot: hash %q vs %q", again.RiskRulesHash, hash)
	}
}

func doWant(t *testing.T, h *harness, method, path, body string, status int, code string) {
	t.Helper()
	resp, got := h.do(t, method, path, body)
	wantCode(t, resp.StatusCode, got, status, code)
}

func TestAPI_ResolvePinnedUnknownHashNoFallback(t *testing.T) {
	h := newHarness(t)
	createAndApprove(t, h, "r-sl", `{"pct":2.5}`)
	if resp, body := h.do(t, http.MethodPost, "/api/risk/resolve", `{"ruleset":"latest"}`); resp.StatusCode != http.StatusOK {
		t.Fatalf("setup resolve: status %d body %s", resp.StatusCode, body)
	}

	// latest 有值，但钉住的 hash 不存在 → 404 snapshot_not_found，绝不回退 latest。
	unknown := strings.Repeat("0", 64)
	doWant(t, h, http.MethodPost, "/api/risk/resolve", `{"hash":"`+unknown+`"}`, http.StatusNotFound, "snapshot_not_found")
	doWant(t, h, http.MethodGet, "/api/risk/snapshots/"+unknown, "", http.StatusNotFound, "snapshot_not_found")
	doWant(t, h, http.MethodGet, "/api/risk/snapshots/"+unknown+"/bundle", "", http.StatusNotFound, "snapshot_not_found")
}

func TestAPI_ResolveWithoutApprovedRules(t *testing.T) {
	h := newHarness(t)
	// 没有任何生效规则：run 拒绝启动（不许用默认限额）。
	doWant(t, h, http.MethodPost, "/api/risk/resolve", `{"ruleset":"latest"}`, http.StatusNotFound, "no_approved_rules")
}

func TestAPI_ResolveRequestValidation(t *testing.T) {
	h := newHarness(t)
	doWant(t, h, http.MethodPost, "/api/risk/resolve", `{"hash":"a","ruleset":"latest"}`, http.StatusBadRequest, "invalid_params")
	doWant(t, h, http.MethodPost, "/api/risk/resolve", `{}`, http.StatusBadRequest, "invalid_params")
	doWant(t, h, http.MethodPost, "/api/risk/resolve", `{"ruleset":"yolo"}`, http.StatusBadRequest, "invalid_params")
	doWant(t, h, http.MethodPost, "/api/risk/resolve", ``, http.StatusBadRequest, "invalid_params")
}

package httpapi

import (
	"encoding/json"
	"net/http"
	"testing"
)

type eventResultWire struct {
	Accepted   int `json:"accepted"`
	Duplicates int `json:"duplicates"`
	Failed     []struct {
		Index   int    `json:"index"`
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"failed"`
	Code string `json:"code"`
}

const eventBody = `{"run_id":"run-1","order_id":"ord-1","event_seq":1,"verdict":"allowed","ts":"2026-10-24T11:59:00Z"}`

func TestAPI_EventsBatchAndDedupe(t *testing.T) {
	h := newHarness(t)

	resp, body := h.do(t, http.MethodPost, "/api/risk/events", `{"events":[`+eventBody+`]}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("first batch: status %d body %s", resp.StatusCode, body)
	}
	first := decode[eventResultWire](t, body)
	if first.Accepted != 1 || first.Duplicates != 0 || len(first.Failed) != 0 || first.Code != "" {
		t.Fatalf("first batch body: %+v", first)
	}

	// 同键重放 = duplicate，不重复落库，仍返回 200。
	resp, body = h.do(t, http.MethodPost, "/api/risk/events", `{"events":[`+eventBody+`]}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("replay: status %d body %s", resp.StatusCode, body)
	}
	replay := decode[eventResultWire](t, body)
	if replay.Accepted != 0 || replay.Duplicates != 1 || len(replay.Failed) != 0 {
		t.Fatalf("replay body: %+v", replay)
	}
}

func TestAPI_EventsPartialFailure(t *testing.T) {
	h := newHarness(t)
	// 第二条缺 run_id：整批不失败，逐条报 207。
	body := `{"events":[` + eventBody + `,{"order_id":"ord-2","event_seq":2,"ts":"2026-10-24T11:59:00Z"}]}`
	resp, out := h.do(t, http.MethodPost, "/api/risk/events", body)
	if resp.StatusCode != http.StatusMultiStatus {
		t.Fatalf("status = %d, want 207 (body %s)", resp.StatusCode, out)
	}
	result := decode[eventResultWire](t, out)
	if result.Code != "partial_failure" || result.Accepted != 1 || len(result.Failed) != 1 {
		t.Fatalf("partial body: %+v", result)
	}
	if result.Failed[0].Index != 1 || result.Failed[0].Code != "invalid_params" || result.Failed[0].Message == "" {
		t.Fatalf("failed item: %+v", result.Failed[0])
	}
}

func TestAPI_EventsRejectsInvalidEnvelope(t *testing.T) {
	h := newHarness(t)
	cases := []string{
		``,                                     // 空 body
		`{}`,                                   // 缺 events
		`{"events":[]}`,                        // 空批次
		`{"events":{}}`,                        // events 不是数组
		`{"events":[` + eventBody + `],"x":1}`, // 未知字段
		`{"events":[` + eventBody + `]} {}`,    // 尾随内容
		`not json`,                             // 不可解析
	}
	for _, body := range cases {
		resp, out := h.do(t, http.MethodPost, "/api/risk/events", body)
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("body %q: status %d, want 400 (%s)", body, resp.StatusCode, out)
		}
		var errWire struct {
			Code string `json:"code"`
		}
		if err := json.Unmarshal([]byte(out), &errWire); err != nil || errWire.Code != "invalid_params" {
			t.Fatalf("body %q: code %q err %v", body, errWire.Code, err)
		}
	}
}

func TestAPI_EventsMethodNotAllowed(t *testing.T) {
	h := newHarness(t)
	if resp, _ := h.do(t, http.MethodGet, "/api/risk/events", ""); resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("GET /api/risk/events = %d, want 405", resp.StatusCode)
	}
}

package event

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"
)

var monitorNow = time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)

func newMonitorService(t *testing.T) (*Service, *Memory) {
	t.Helper()
	store := NewMemory()
	svc, err := New(Options{Store: store, Now: func() time.Time { return monitorNow }})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return svc, store
}

func mustIngest(t *testing.T, svc *Service, raws ...string) {
	t.Helper()
	batch := make([]json.RawMessage, 0, len(raws))
	for _, raw := range raws {
		batch = append(batch, json.RawMessage(raw))
	}
	res, err := svc.Ingest(context.Background(), batch)
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	if len(res.Failed) != 0 {
		t.Fatalf("Ingest failures: %+v", res.Failed)
	}
}

func exposureFixture(t *testing.T) (*Service, *Memory) {
	t.Helper()
	svc, store := newMonitorService(t)
	mustIngest(t, svc,
		`{"run_id":"run-1","order_id":"o-buy","event_seq":1,"symbol":"600000.SH","verdict":"allowed","ts":"2026-10-24T11:00:00Z","payload":{"side":"buy","qty":100,"price":10}}`,
		`{"run_id":"run-1","order_id":"o-sell","event_seq":1,"symbol":"600000.SH","verdict":"allowed","ts":"2026-10-24T11:01:00Z","payload":{"side":"sell","qty":40,"price":10}}`,
		`{"run_id":"run-1","order_id":"o-notional","event_seq":1,"symbol":"000001.SZ","verdict":"allowed","ts":"2026-10-24T11:02:00Z","payload":{"notional":500}}`,
		`{"run_id":"run-1","order_id":"o-unpriced","event_seq":1,"symbol":"000001.SZ","verdict":"allowed","ts":"2026-10-24T11:03:00Z","payload":{"account":"acct-1"}}`,
		`{"run_id":"run-1","order_id":"o-alert","event_seq":1,"symbol":"600000.SH","verdict":"alert","ts":"2026-10-24T11:04:00Z","payload":{"rule":"drawdown","severity":"warning","message":"dd","notional":999}}`,
		`{"run_id":"run-2","order_id":"o-other-run","event_seq":1,"symbol":"600000.SH","verdict":"allowed","ts":"2026-10-24T11:05:00Z","payload":{"side":"buy","notional":100}}`,
	)
	return svc, store
}

func TestExposure_AggregatesFromRiskEvents(t *testing.T) {
	svc, _ := exposureFixture(t)
	rep, err := svc.Exposure(context.Background(), ExposureQuery{RunID: "run-1"})
	if err != nil {
		t.Fatalf("Exposure: %v", err)
	}
	if rep.Source != MonitorSource || rep.GroupBy != GroupBySymbol {
		t.Fatalf("report header = %+v", rep)
	}
	if rep.Window.From != monitorNow.Add(-DefaultMonitorWindow) || !rep.Window.To.Equal(monitorNow) {
		t.Fatalf("default window = %+v", rep.Window)
	}
	if rep.Events != 3 || rep.UnpricedEvents != 1 || rep.UnsignedEvents != 1 {
		t.Fatalf("event counters = events %d unpriced %d unsigned %d", rep.Events, rep.UnpricedEvents, rep.UnsignedEvents)
	}
	if rep.Totals.GrossNotional != 1900 || rep.Totals.NetNotional != 600 ||
		rep.Totals.LongNotional != 1000 || rep.Totals.ShortNotional != 400 {
		t.Fatalf("totals = %+v", rep.Totals)
	}
	if len(rep.Groups) != 2 || rep.Groups[0].Key != "000001.SZ" || rep.Groups[1].Key != "600000.SH" {
		t.Fatalf("groups must be sorted by key: %+v", rep.Groups)
	}
	sz := rep.Groups[0]
	if sz.Events != 1 || sz.UnpricedEvents != 1 || sz.UnsignedEvents != 1 ||
		sz.GrossNotional != 500 || sz.NetNotional != 0 {
		t.Fatalf("000001.SZ = %+v", sz)
	}
	sh := rep.Groups[1]
	if sh.Events != 2 || sh.GrossNotional != 1400 || sh.NetNotional != 600 ||
		sh.LongNotional != 1000 || sh.ShortNotional != 400 {
		t.Fatalf("600000.SH = %+v", sh)
	}
}

func TestExposure_GroupByAccountUsesExplicitBucket(t *testing.T) {
	svc, _ := exposureFixture(t)
	rep, err := svc.Exposure(context.Background(), ExposureQuery{RunID: "run-1", GroupBy: GroupByAccount})
	if err != nil {
		t.Fatalf("Exposure: %v", err)
	}
	if len(rep.Groups) != 2 || rep.Groups[0].Key != "acct-1" || rep.Groups[1].Key != UnattributedGroup {
		t.Fatalf("account groups = %+v", rep.Groups)
	}
	if rep.Groups[0].Events != 0 || rep.Groups[0].UnpricedEvents != 1 {
		t.Fatalf("acct-1 = %+v", rep.Groups[0])
	}
	if rep.Groups[1].Events != 3 || rep.Groups[1].GrossNotional != 1900 {
		t.Fatalf("unattributed = %+v", rep.Groups[1])
	}
}

func TestExposure_RejectsBadWindowsAndGroupBy(t *testing.T) {
	svc, _ := exposureFixture(t)
	cases := []struct {
		name string
		q    ExposureQuery
	}{
		{"bad group by", ExposureQuery{GroupBy: "planet"}},
		{"from after to", ExposureQuery{From: monitorNow, To: monitorNow.Add(-time.Hour)}},
		{"window too large", ExposureQuery{From: monitorNow.Add(-MaxMonitorWindow - time.Minute), To: monitorNow}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := svc.Exposure(context.Background(), tc.q); !errors.Is(err, ErrInvalidParams) {
				t.Fatalf("want ErrInvalidParams, got %v", err)
			}
		})
	}
}

func TestExposure_RefusesTruncatedAggregate(t *testing.T) {
	svc, _ := newMonitorService(t)
	raws := make([]string, 0, MaxExposureEvents+1)
	for i := 0; i < MaxExposureEvents+1; i++ {
		raws = append(raws, fmt.Sprintf(
			`{"run_id":"run-1","order_id":"o-%d","event_seq":1,"symbol":"X","verdict":"allowed","ts":"2026-10-24T11:00:00Z","payload":{"notional":1}}`, i))
	}
	mustIngest(t, svc, raws...)
	if _, err := svc.Exposure(context.Background(), ExposureQuery{}); !errors.Is(err, ErrInvalidParams) {
		t.Fatalf("truncated exposure must be rejected, got %v", err)
	}
}

type writeOnlyStore struct{}

func (writeOnlyStore) PutEvents(context.Context, []Record) (int, int, error) { return 0, 0, nil }

func TestMonitor_RequiresReader(t *testing.T) {
	svc, err := New(Options{Store: writeOnlyStore{}, Now: func() time.Time { return monitorNow }})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := svc.Exposure(context.Background(), ExposureQuery{}); !errors.Is(err, ErrMonitorUnavailable) {
		t.Fatalf("Exposure: want ErrMonitorUnavailable, got %v", err)
	}
	if _, err := svc.Alerts(context.Background(), AlertsQuery{}); !errors.Is(err, ErrMonitorUnavailable) {
		t.Fatalf("Alerts: want ErrMonitorUnavailable, got %v", err)
	}
	if _, err := svc.Evaluate(context.Background(), EvaluateQuery{}); !errors.Is(err, ErrMonitorUnavailable) {
		t.Fatalf("Evaluate: want ErrMonitorUnavailable, got %v", err)
	}
}

func alertsFixture(t *testing.T) (*Service, *Memory) {
	t.Helper()
	svc, store := newMonitorService(t)
	mustIngest(t, svc,
		`{"run_id":"run-1","order_id":"o-a","event_seq":1,"symbol":"600000.SH","verdict":"alert","ts":"2026-10-24T11:00:00Z","payload":{"id":"a1","rule":"drawdown","severity":"warning","message":"drawdown 12%","value":0.12,"threshold":0.1,"attributes":{"peak":1.2}}}`,
		`{"run_id":"run-1","order_id":"o-b","event_seq":1,"symbol":"000001.SZ","verdict":"alert","ts":"2026-10-24T11:05:00Z","payload":{"rule":"position_concentration","severity":"critical","message":"position 40%"}}`,
		`{"run_id":"run-1","order_id":"o-c","event_seq":1,"symbol":"000001.SZ","verdict":"alert","ts":"2026-10-24T11:10:00Z","payload":{"rule":"x","severity":"bogus","message":"bad severity"}}`,
		`{"run_id":"run-1","order_id":"o-d","event_seq":1,"symbol":"000001.SZ","verdict":"alert","ts":"2026-10-24T11:11:00Z","payload":{"severity":"warning","message":"missing rule"}}`,
		`{"run_id":"run-1","order_id":"o-e","event_seq":1,"symbol":"600000.SH","verdict":"allowed","ts":"2026-10-24T11:12:00Z","payload":{"rule":"drawdown","severity":"warning","message":"not an alert"}}`,
	)
	return svc, store
}

func TestAlerts_ProjectsAndSortsFromRiskEvents(t *testing.T) {
	svc, _ := alertsFixture(t)
	rep, err := svc.Alerts(context.Background(), AlertsQuery{})
	if err != nil {
		t.Fatalf("Alerts: %v", err)
	}
	if rep.Source != MonitorSource || rep.Count != 2 || len(rep.Alerts) != 2 {
		t.Fatalf("report = %+v", rep)
	}
	if rep.InvalidEvents != 2 {
		t.Fatalf("invalid_events = %d, want 2", rep.InvalidEvents)
	}
	if rep.Alerts[0].ID != "run-1:o-b:1" || rep.Alerts[0].Severity != SeverityCritical {
		t.Fatalf("first alert (newest) = %+v", rep.Alerts[0])
	}
	if rep.Alerts[1].ID != "a1" || rep.Alerts[1].Value == nil || *rep.Alerts[1].Value != 0.12 {
		t.Fatalf("second alert = %+v", rep.Alerts[1])
	}
	if string(rep.Alerts[1].Attributes) != `{"peak":1.2}` {
		t.Fatalf("attributes = %s", rep.Alerts[1].Attributes)
	}
}

func TestAlerts_FiltersAndLimit(t *testing.T) {
	svc, _ := alertsFixture(t)

	rep, err := svc.Alerts(context.Background(), AlertsQuery{Severity: SeverityWarning})
	if err != nil || rep.Count != 1 || rep.Alerts[0].ID != "a1" {
		t.Fatalf("severity filter: %+v err %v", rep, err)
	}
	rep, err = svc.Alerts(context.Background(), AlertsQuery{Rule: "position_concentration"})
	if err != nil || rep.Count != 1 || rep.Alerts[0].ID != "run-1:o-b:1" {
		t.Fatalf("rule filter: %+v err %v", rep, err)
	}
	rep, err = svc.Alerts(context.Background(), AlertsQuery{Symbol: "000001.SZ"})
	if err != nil || rep.Count != 1 {
		t.Fatalf("symbol filter: %+v err %v", rep, err)
	}
	rep, err = svc.Alerts(context.Background(), AlertsQuery{Limit: 1})
	if err != nil || rep.Count != 1 || !rep.Truncated {
		t.Fatalf("limit: %+v err %v", rep, err)
	}
	rep, err = svc.Alerts(context.Background(), AlertsQuery{RunID: "nope"})
	if err != nil || rep.Count != 0 || rep.Alerts == nil {
		t.Fatalf("empty result must be []: %+v err %v", rep, err)
	}

	if _, err := svc.Alerts(context.Background(), AlertsQuery{Severity: "bogus"}); !errors.Is(err, ErrInvalidParams) {
		t.Fatalf("bad severity: want ErrInvalidParams, got %v", err)
	}
	if _, err := svc.Alerts(context.Background(), AlertsQuery{Limit: -1}); !errors.Is(err, ErrInvalidParams) {
		t.Fatalf("bad limit: want ErrInvalidParams, got %v", err)
	}
	if _, err := svc.Alerts(context.Background(), AlertsQuery{Limit: MaxAlertLimit + 1}); !errors.Is(err, ErrInvalidParams) {
		t.Fatalf("limit over max: want ErrInvalidParams, got %v", err)
	}
}

func TestEvaluate_ReportsWithoutWritingOrDetecting(t *testing.T) {
	svc, store := alertsFixture(t)
	before := len(store.stored())

	rep, err := svc.Evaluate(context.Background(), EvaluateQuery{MinSeverity: SeverityWarning})
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if rep.Detection != DetectionOutsideProcess || rep.Source != MonitorSource {
		t.Fatalf("evaluation must declare external detection: %+v", rep)
	}
	if rep.Summary.Total != 2 || rep.Summary.BySeverity[SeverityWarning] != 1 ||
		rep.Summary.BySeverity[SeverityCritical] != 1 || rep.Summary.ByRule["drawdown"] != 1 {
		t.Fatalf("summary = %+v", rep.Summary)
	}
	if rep.InvalidEvents != 2 {
		t.Fatalf("invalid_events = %d, want 2", rep.InvalidEvents)
	}
	if got := len(store.stored()); got != before {
		t.Fatalf("Evaluate must not write: rows %d -> %d", before, got)
	}

	rep, err = svc.Evaluate(context.Background(), EvaluateQuery{MinSeverity: SeverityCritical})
	if err != nil || rep.Summary.Total != 1 {
		t.Fatalf("critical floor: %+v err %v", rep, err)
	}
	rep, err = svc.Evaluate(context.Background(), EvaluateQuery{})
	if err != nil || rep.Criteria.MinSeverity != SeverityInfo || rep.Summary.Total != 2 {
		t.Fatalf("default floor must be info: %+v err %v", rep, err)
	}

	if _, err := svc.Evaluate(context.Background(), EvaluateQuery{MinSeverity: "bogus"}); !errors.Is(err, ErrInvalidParams) {
		t.Fatalf("bad min_severity: want ErrInvalidParams, got %v", err)
	}
	if _, err := svc.Evaluate(context.Background(), EvaluateQuery{Limit: 1}); !errors.Is(err, ErrInvalidParams) {
		t.Fatalf("truncated evaluation must be rejected, got %v", err)
	}
}

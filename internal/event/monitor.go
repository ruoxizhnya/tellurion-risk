package event

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"
)

// ErrMonitorUnavailable 表示没有装配 risk_events 读取端口。
// 监控端点必须据此拒绝服务，不许拿空集冒充「没有敞口 / 没有告警」。
var ErrMonitorUnavailable = errors.New("event: monitor reader is not configured")

// 监控投影的冻结词表。告警优先级与 core pkg/alert 的 Severity 同源。
const (
	GroupBySymbol   = "symbol"
	GroupByAccount  = "account"
	GroupByStrategy = "strategy"
	// UnattributedGroup 是分组键缺失时的显式桶名（不静默丢事件）。
	UnattributedGroup = "unattributed"
	// VerdictAlert 标记一条事件是 detector 产出的告警（检测器本身在 core 进程内）。
	VerdictAlert = "alert"

	SeverityInfo     = "info"
	SeverityWarning  = "warning"
	SeverityCritical = "critical"

	// DetectionOutsideProcess 出现在 evaluate 响应里：本仓不运行检测器。
	DetectionOutsideProcess = "outside_process"
	// MonitorSource 明确数据来源（验收：敞口 / 告警数据来自 risk.risk_events）。
	MonitorSource = "risk.risk_events"

	DefaultMonitorWindow = 24 * time.Hour
	MaxMonitorWindow     = 7 * 24 * time.Hour
	DefaultAlertLimit    = 100
	MaxAlertLimit        = 1000
	// MaxExposureEvents 是敞口聚合的硬上限：超过就拒绝而不是截断，
	// 因为截断的敞口会低报风险。
	MaxExposureEvents = 5000
)

// Window 是查询的时间窗（闭区间，按事件时间 event_ts）。
type Window struct {
	From time.Time `json:"from"`
	To   time.Time `json:"to"`
}

// ExposureQuery 是敞口聚合的输入。From/To 零值取默认窗口。
type ExposureQuery struct {
	From    time.Time
	To      time.Time
	GroupBy string
	RunID   string
	Symbol  string
	Verdict string
}

// ExposureTotals 是聚合口径的四个量（金额单位由事件 payload 决定，本服务不换算）。
type ExposureTotals struct {
	GrossNotional float64 `json:"gross_notional"`
	NetNotional   float64 `json:"net_notional"`
	LongNotional  float64 `json:"long_notional"`
	ShortNotional float64 `json:"short_notional"`
}

// ExposureGroup 是按 group_by 分桶后的敞口。
type ExposureGroup struct {
	Key            string  `json:"key"`
	Events         int     `json:"events"`
	UnpricedEvents int     `json:"unpriced_events"`
	UnsignedEvents int     `json:"unsigned_events"`
	GrossNotional  float64 `json:"gross_notional"`
	NetNotional    float64 `json:"net_notional"`
	LongNotional   float64 `json:"long_notional"`
	ShortNotional  float64 `json:"short_notional"`
}

// ExposureReport 是 GET /monitor/exposure 的响应。
type ExposureReport struct {
	Window         Window          `json:"window"`
	GroupBy        string          `json:"group_by"`
	Source         string          `json:"source"`
	Events         int             `json:"events"`
	UnpricedEvents int             `json:"unpriced_events"`
	UnsignedEvents int             `json:"unsigned_events"`
	Totals         ExposureTotals  `json:"totals"`
	Groups         []ExposureGroup `json:"groups"`
}

// AlertsQuery 是告警读侧的输入。
type AlertsQuery struct {
	From     time.Time
	To       time.Time
	Severity string
	Rule     string
	RunID    string
	Symbol   string
	Limit    int
}

// Alert 是一条告警事件的投影。字段词表与 core pkg/alert.Alert 对齐。
type Alert struct {
	ID         string          `json:"id"`
	Rule       string          `json:"rule"`
	Severity   string          `json:"severity"`
	Message    string          `json:"message"`
	Value      *float64        `json:"value,omitempty"`
	Threshold  *float64        `json:"threshold,omitempty"`
	Symbol     string          `json:"symbol,omitempty"`
	RunID      string          `json:"run_id"`
	OrderID    string          `json:"order_id"`
	EventSeq   int64           `json:"event_seq"`
	Ts         time.Time       `json:"ts"`
	Attributes json.RawMessage `json:"attributes,omitempty"`
}

// AlertReport 是 GET /monitor/alerts 的响应。
type AlertReport struct {
	Window        Window  `json:"window"`
	Source        string  `json:"source"`
	Alerts        []Alert `json:"alerts"`
	Count         int     `json:"count"`
	Truncated     bool    `json:"truncated"`
	InvalidEvents int     `json:"invalid_events"`
}

// EvaluateQuery 是 POST /monitor/evaluate 的输入。本端点不运行检测器，
// 只按给定口径对已回传的告警事件做一次报表评估。
type EvaluateQuery struct {
	From        time.Time
	To          time.Time
	RunID       string
	Rule        string
	Symbol      string
	MinSeverity string
	Limit       int
}

// EvaluationCriteria 回显本次评估口径。
type EvaluationCriteria struct {
	MinSeverity string `json:"min_severity"`
}

// EvaluationSummary 是按严重度 / 规则聚合的计数。
type EvaluationSummary struct {
	Total      int            `json:"total"`
	BySeverity map[string]int `json:"by_severity"`
	ByRule     map[string]int `json:"by_rule"`
}

// EvaluationReport 是 POST /monitor/evaluate 的响应。
type EvaluationReport struct {
	EvaluatedAt   time.Time          `json:"evaluated_at"`
	Window        Window             `json:"window"`
	Source        string             `json:"source"`
	Detection     string             `json:"detection"`
	Criteria      EvaluationCriteria `json:"criteria"`
	Alerts        []Alert            `json:"alerts"`
	Summary       EvaluationSummary  `json:"summary"`
	InvalidEvents int                `json:"invalid_events"`
}

// monitorPayload 是 risk_events.payload 里监控认识的字段子集。
// 未知字段不影响解析（payload 仍是调用方的自由审计载荷）。
type monitorPayload struct {
	Account    string          `json:"account"`
	Strategy   string          `json:"strategy"`
	Side       string          `json:"side"`
	Qty        *float64        `json:"qty"`
	Price      *float64        `json:"price"`
	Notional   *float64        `json:"notional"`
	ID         string          `json:"id"`
	Rule       string          `json:"rule"`
	Severity   string          `json:"severity"`
	Message    string          `json:"message"`
	Value      *float64        `json:"value"`
	Threshold  *float64        `json:"threshold"`
	Attributes json.RawMessage `json:"attributes"`
}

// Exposure 聚合指定窗口内的事件。同一条事件只在一个分组里计数；
// 缺 notional / qty+price 的事件计入 unpriced_events，不补零。
func (s *Service) Exposure(ctx context.Context, q ExposureQuery) (ExposureReport, error) {
	win, err := s.monitorWindow(q.From, q.To)
	if err != nil {
		return ExposureReport{}, err
	}
	groupBy := q.GroupBy
	if groupBy == "" {
		groupBy = GroupBySymbol
	}
	switch groupBy {
	case GroupBySymbol, GroupByAccount, GroupByStrategy:
	default:
		return ExposureReport{}, fmt.Errorf("%w: group_by must be symbol, account or strategy", ErrInvalidParams)
	}
	if s.reader == nil {
		return ExposureReport{}, ErrMonitorUnavailable
	}

	records, truncated, err := s.reader.QueryEvents(ctx, Query{
		From: win.From, To: win.To, RunID: q.RunID, Symbol: q.Symbol,
		Verdict: q.Verdict, Limit: MaxExposureEvents + 1,
	})
	if err != nil {
		return ExposureReport{}, err
	}
	if truncated || len(records) > MaxExposureEvents {
		return ExposureReport{}, fmt.Errorf(
			"%w: more than %d events in window; narrow the window", ErrInvalidParams, MaxExposureEvents)
	}

	report := ExposureReport{
		Window: win, GroupBy: groupBy, Source: MonitorSource,
		Groups: make([]ExposureGroup, 0),
	}
	groups := make(map[string]*ExposureGroup)
	for _, rec := range records {
		// 告警是发现，不是成交；计入敞口会与裁决事件重复计数。
		if rec.Verdict == VerdictAlert {
			continue
		}
		payload := decodeMonitorPayload(rec.Payload)

		key := groupKey(rec, payload, groupBy)
		g, ok := groups[key]
		if !ok {
			g = &ExposureGroup{Key: key}
			groups[key] = g
		}

		notional, side, priced := exposureOf(payload)
		if !priced {
			g.UnpricedEvents++
			report.UnpricedEvents++
			continue
		}
		g.Events++
		report.Events++
		g.GrossNotional += notional
		report.Totals.GrossNotional += notional
		switch side {
		case "buy":
			g.LongNotional += notional
			g.NetNotional += notional
			report.Totals.LongNotional += notional
			report.Totals.NetNotional += notional
		case "sell":
			g.ShortNotional += notional
			g.NetNotional -= notional
			report.Totals.ShortNotional += notional
			report.Totals.NetNotional -= notional
		default:
			g.UnsignedEvents++
			report.UnsignedEvents++
		}
	}

	for _, g := range groups {
		report.Groups = append(report.Groups, *g)
	}
	sort.Slice(report.Groups, func(i, j int) bool { return report.Groups[i].Key < report.Groups[j].Key })
	return report, nil
}

// Alerts 读取窗口内的告警事件（verdict=alert 且 payload 投影合法）。
// 非法投影计入 invalid_events，不静默丢弃；limit 截断显式标记 truncated。
func (s *Service) Alerts(ctx context.Context, q AlertsQuery) (AlertReport, error) {
	win, err := s.monitorWindow(q.From, q.To)
	if err != nil {
		return AlertReport{}, err
	}
	if q.Severity != "" {
		if _, ok := severityRank(q.Severity); !ok {
			return AlertReport{}, fmt.Errorf("%w: severity must be info, warning or critical", ErrInvalidParams)
		}
	}
	limit, err := normalizeLimit(q.Limit, DefaultAlertLimit, MaxAlertLimit)
	if err != nil {
		return AlertReport{}, err
	}
	scan, err := s.scanAlerts(ctx, win, q, limit)
	if err != nil {
		return AlertReport{}, err
	}
	return AlertReport{
		Window: win, Source: MonitorSource, Alerts: scan.alerts,
		Count: len(scan.alerts), Truncated: scan.truncated, InvalidEvents: scan.invalid,
	}, nil
}

// Evaluate 对已回传的告警事件做一次报表评估：按 min_severity 过滤并汇总。
// 它不运行任何检测器（检测器在 core 进程内），也不写库 —— 可安全重试。
func (s *Service) Evaluate(ctx context.Context, q EvaluateQuery) (EvaluationReport, error) {
	win, err := s.monitorWindow(q.From, q.To)
	if err != nil {
		return EvaluationReport{}, err
	}
	minSeverity := q.MinSeverity
	if minSeverity == "" {
		minSeverity = SeverityInfo
	}
	minRank, ok := severityRank(minSeverity)
	if !ok {
		return EvaluationReport{}, fmt.Errorf("%w: min_severity must be info, warning or critical", ErrInvalidParams)
	}
	limit, err := normalizeLimit(q.Limit, MaxAlertLimit, MaxAlertLimit)
	if err != nil {
		return EvaluationReport{}, err
	}
	scan, err := s.scanAlerts(ctx, win, AlertsQuery{
		RunID: q.RunID, Rule: q.Rule, Symbol: q.Symbol,
	}, limit)
	if err != nil {
		return EvaluationReport{}, err
	}
	// 汇总报告不允许建立在截断数据上：宁可让调用方缩小窗口。
	if scan.truncated {
		return EvaluationReport{}, fmt.Errorf(
			"%w: more than %d alert events in window; narrow the window", ErrInvalidParams, limit)
	}

	report := EvaluationReport{
		EvaluatedAt: s.now().UTC(),
		Window:      win,
		Source:      MonitorSource,
		Detection:   DetectionOutsideProcess,
		Criteria:    EvaluationCriteria{MinSeverity: minSeverity},
		Alerts:      make([]Alert, 0, len(scan.alerts)),
		Summary: EvaluationSummary{
			BySeverity: make(map[string]int),
			ByRule:     make(map[string]int),
		},
		InvalidEvents: scan.invalid,
	}
	for _, a := range scan.alerts {
		rank, _ := severityRank(a.Severity)
		if rank < minRank {
			continue
		}
		report.Alerts = append(report.Alerts, a)
		report.Summary.Total++
		report.Summary.BySeverity[a.Severity]++
		report.Summary.ByRule[a.Rule]++
	}
	return report, nil
}

type alertScan struct {
	alerts    []Alert
	invalid   int
	truncated bool
}

func (s *Service) scanAlerts(ctx context.Context, win Window, q AlertsQuery, limit int) (alertScan, error) {
	if s.reader == nil {
		return alertScan{}, ErrMonitorUnavailable
	}
	// limit 作用在**合法投影**上（分页语义）；原始行另有 MaxAlertLimit 的扫描上限，
	// 否则少量脏数据就能把整页挤空。触到扫描上限时显式 truncated，不静默。
	records, rawTruncated, err := s.reader.QueryEvents(ctx, Query{
		From: win.From, To: win.To, RunID: q.RunID, Symbol: q.Symbol,
		Verdict: VerdictAlert, Limit: MaxAlertLimit + 1,
	})
	if err != nil {
		return alertScan{}, err
	}
	if len(records) > MaxAlertLimit {
		records = records[:MaxAlertLimit]
		rawTruncated = true
	}

	scan := alertScan{alerts: make([]Alert, 0, len(records)), truncated: rawTruncated}
	for _, rec := range records {
		alert, ok := projectAlert(rec)
		if !ok {
			scan.invalid++
			continue
		}
		if q.Severity != "" && alert.Severity != q.Severity {
			continue
		}
		if q.Rule != "" && alert.Rule != q.Rule {
			continue
		}
		scan.alerts = append(scan.alerts, alert)
	}
	if len(scan.alerts) > limit {
		scan.alerts = scan.alerts[:limit]
		scan.truncated = true
	}
	return scan, nil
}

// projectAlert 把一条 verdict=alert 的事件投影成告警。字段缺失或词表外的值
// 一律视为非法投影（由调用方计入 invalid_events），不做默认值兜底。
func projectAlert(rec Record) (Alert, bool) {
	p := decodeMonitorPayload(rec.Payload)
	rule := strings.TrimSpace(p.Rule)
	if rule == "" {
		return Alert{}, false
	}
	severity := strings.TrimSpace(p.Severity)
	if _, ok := severityRank(severity); !ok {
		return Alert{}, false
	}
	message := strings.TrimSpace(p.Message)
	if message == "" {
		return Alert{}, false
	}
	id := strings.TrimSpace(p.ID)
	if id == "" {
		id = rec.RunID + ":" + rec.OrderID + ":" + strconv.FormatInt(rec.EventSeq, 10)
	}
	alert := Alert{
		ID:        id,
		Rule:      rule,
		Severity:  severity,
		Message:   message,
		Value:     p.Value,
		Threshold: p.Threshold,
		Symbol:    rec.Symbol,
		RunID:     rec.RunID,
		OrderID:   rec.OrderID,
		EventSeq:  rec.EventSeq,
		Ts:        rec.Ts,
	}
	if len(p.Attributes) > 0 && string(p.Attributes) != "null" {
		alert.Attributes = append(json.RawMessage(nil), p.Attributes...)
	}
	return alert, true
}

func decodeMonitorPayload(raw json.RawMessage) monitorPayload {
	var p monitorPayload
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &p)
	}
	return p
}

// exposureOf 返回绝对名义金额与方向；priced=false 表示字段不足以计算，不得补零。
func exposureOf(p monitorPayload) (notional float64, side string, priced bool) {
	switch {
	case p.Notional != nil:
		notional = math.Abs(*p.Notional)
	case p.Qty != nil && p.Price != nil:
		notional = math.Abs(*p.Qty) * math.Abs(*p.Price)
	default:
		return 0, "", false
	}
	side = p.Side
	if side == "" && p.Qty != nil {
		switch {
		case *p.Qty > 0:
			side = "buy"
		case *p.Qty < 0:
			side = "sell"
		}
	}
	if side != "buy" && side != "sell" {
		side = ""
	}
	return notional, side, true
}

func groupKey(rec Record, p monitorPayload, groupBy string) string {
	var key string
	switch groupBy {
	case GroupByAccount:
		key = p.Account
	case GroupByStrategy:
		key = p.Strategy
	default:
		key = rec.Symbol
	}
	if strings.TrimSpace(key) == "" {
		return UnattributedGroup
	}
	return key
}

func severityRank(severity string) (int, bool) {
	switch severity {
	case SeverityInfo:
		return 1, true
	case SeverityWarning:
		return 2, true
	case SeverityCritical:
		return 3, true
	default:
		return 0, false
	}
}

func (s *Service) monitorWindow(from, to time.Time) (Window, error) {
	if to.IsZero() {
		to = s.now().UTC()
	}
	if from.IsZero() {
		from = to.Add(-DefaultMonitorWindow)
	}
	if !from.Before(to) {
		return Window{}, fmt.Errorf("%w: from must be before to", ErrInvalidParams)
	}
	if to.Sub(from) > MaxMonitorWindow {
		return Window{}, fmt.Errorf("%w: window exceeds %s", ErrInvalidParams, MaxMonitorWindow)
	}
	return Window{From: from.UTC(), To: to.UTC()}, nil
}

func normalizeLimit(v, def, max int) (int, error) {
	if v == 0 {
		return def, nil
	}
	if v < 0 || v > max {
		return 0, fmt.Errorf("%w: limit must be between 1 and %d", ErrInvalidParams, max)
	}
	return v, nil
}

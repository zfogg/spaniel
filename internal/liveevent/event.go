// Package liveevent defines the durable, transport-neutral /ws contract.
package liveevent

import (
	"encoding/json"
	"time"
)

// Typed payload structs

type SpanPayload struct {
	TraceID     string            `json:"traceId"`
	SpanID      string            `json:"spanId"`
	ServiceName string            `json:"serviceName"`
	Name        string            `json:"name"`
	DurationNs  int64             `json:"durationNs"`
	StatusCode  int               `json:"statusCode"`
	SessionID   string            `json:"sessionId"`
	Row         *SelfSpanPayload  `json:"row,omitempty"`
	Trace       *SelfTracePayload `json:"trace,omitempty"`
}

// SelfTracePayload is a complete trace-list row sent only for Spaniel's own
// telemetry. Clients patch it into their cached first page instead of issuing
// an HTTP refetch, which prevents the UI read from generating another
// self-observability trace and creating a refresh loop.
type SelfTracePayload struct {
	TraceID      string   `json:"trace_id"`
	ServiceName  string   `json:"service_name"`
	Name         string   `json:"name"`
	Attributes   string   `json:"attributes"`
	StatusCode   int      `json:"status_code"`
	StartNs      int64    `json:"start_ns"`
	EndNs        int64    `json:"end_ns"`
	DurationNs   int64    `json:"duration_ns"`
	SessionID    string   `json:"session_id"`
	SessionLabel string   `json:"session_label"`
	HasN1        bool     `json:"has_n1"`
	SpanCount    int      `json:"span_count"`
	IssueKinds   []string `json:"issue_kinds"`
}

// SelfSpanPayload is a span-list row sent directly to clients so self
// observability can update the Span view without causing a read/write loop.
type SelfSpanPayload struct {
	TraceID       string `json:"trace_id"`
	SpanID        string `json:"span_id"`
	ParentSpanID  string `json:"parent_span_id"`
	ServiceName   string `json:"service_name"`
	Name          string `json:"name"`
	Kind          int    `json:"kind"`
	StartNs       int64  `json:"start_ns"`
	EndNs         int64  `json:"end_ns"`
	DurationNs    int64  `json:"duration_ns"`
	StatusCode    int    `json:"status_code"`
	StatusMessage string `json:"status_message"`
	Attributes    string `json:"attributes"`
	Resource      string `json:"resource"`
	SessionID     string `json:"session_id"`
	SessionLabel  string `json:"session_label"`
	ReceivedAt    int64  `json:"received_at"`
	Events        []any  `json:"events"`
	Links         []any  `json:"links"`
}

// SelfLogPayload is the complete log-list row for a self-observability log.
type SelfLogPayload struct {
	TimestampNs int64  `json:"timestamp_ns"`
	TraceID     string `json:"trace_id"`
	SpanID      string `json:"span_id"`
	Severity    int    `json:"severity"`
	Body        string `json:"body"`
	Attributes  string `json:"attributes"`
	ServiceName string `json:"service_name"`
	SessionID   string `json:"session_id"`
	ReceivedAt  int64  `json:"received_at"`
}

type LogPayload struct {
	TraceID     string          `json:"traceId"`
	SpanID      string          `json:"spanId"`
	Severity    int             `json:"severity"`
	Body        string          `json:"body"`
	ServiceName string          `json:"serviceName"`
	SessionID   string          `json:"sessionId"`
	Row         *SelfLogPayload `json:"row,omitempty"`
}

type MetricPayload struct {
	Name        string  `json:"name"`
	ServiceName string  `json:"serviceName"`
	Value       float64 `json:"value"`
	Type        string  `json:"type"`
	// CatalogOnly announces a stored self-telemetry point without inviting the
	// client to refetch metric queries (which would create more self telemetry).
	CatalogOnly bool `json:"catalogOnly,omitempty"`
	// Row is the durable point exactly as decoded from OTLP. It lets browser
	// clients update their caches from the post-persist notification without
	// immediately reading the same point back from storage.
	Row any `json:"row,omitempty"`
}

type IssuePayload struct {
	TraceID     string `json:"traceId"`
	Kind        string `json:"kind"`
	Fingerprint string `json:"fingerprint"`
	Count       int    `json:"count"`
	WastedNs    int64  `json:"wastedNs"`
}

type ForwarderPayload struct {
	URL          string `json:"url"`
	Sent         int64  `json:"sent"`
	Errors       int64  `json:"errors"`
	LastErr      string `json:"lastError,omitempty"`
	PendingBytes int64  `json:"pendingBytes,omitempty"`
	DroppedSpool int64  `json:"droppedSpool,omitempty"`
}

type ThroughputPayload struct {
	SpansPerSec float64 `json:"spansPerSec"`
	LogsPerSec  float64 `json:"logsPerSec"`
}

// LiveStatePayload is a complete replacement for the small, high-churn footer
// resources.  Keeping the snapshots together lets clients bootstrap over HTTP
// once and then update their cache without turning each ingest into more HTTP.
// The concrete data lives in storage, so it remains deliberately transport
// neutral here.
type LiveStatePayload struct {
	Stats   any `json:"stats"`
	Sources any `json:"sources"`
}

type ActiveSessionPayload struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

// Event is the outer discriminated union sent over the WebSocket.
type Event struct {
	Type      string `json:"type"`
	Timestamp int64  `json:"timestamp_ns"`
	Payload   any    `json:"payload"`
}

// SpanEvent is kept as a backward-compat alias for SpanPayload.
type SpanEvent = SpanPayload

// Constructor helpers

func NewSpanEvent(p *SpanPayload) *Event {
	return &Event{Type: "span", Timestamp: time.Now().UnixNano(), Payload: p}
}

func NewSelfTraceEvent(p *SelfTracePayload) *Event {
	return &Event{Type: "self_trace", Timestamp: time.Now().UnixNano(), Payload: p}
}

func NewSelfSpanEvent(p *SelfSpanPayload) *Event {
	return &Event{Type: "self_span", Timestamp: time.Now().UnixNano(), Payload: p}
}

func NewSelfLogEvent(p *SelfLogPayload) *Event {
	return &Event{Type: "self_log", Timestamp: time.Now().UnixNano(), Payload: p}
}

func NewLogEvent(p *LogPayload) *Event {
	return &Event{Type: "log", Timestamp: time.Now().UnixNano(), Payload: p}
}

func NewMetricEvent(p *MetricPayload) *Event {
	return &Event{Type: "metric", Timestamp: time.Now().UnixNano(), Payload: p}
}

func NewIssueEvent(p *IssuePayload) *Event {
	return &Event{Type: "issue", Timestamp: time.Now().UnixNano(), Payload: p}
}

func NewForwarderEvent(p *ForwarderPayload) *Event {
	return &Event{Type: "forwarder", Timestamp: time.Now().UnixNano(), Payload: p}
}

func NewThroughputEvent(p *ThroughputPayload) *Event {
	return &Event{Type: "throughput", Timestamp: time.Now().UnixNano(), Payload: p}
}

func NewLiveStateEvent(p *LiveStatePayload) *Event {
	return &Event{Type: "live_state", Timestamp: time.Now().UnixNano(), Payload: p}
}

func NewActiveSessionEvent(p *ActiveSessionPayload) *Event {
	return &Event{Type: "active_session", Timestamp: time.Now().UnixNano(), Payload: p}
}

// NewHeartbeatEvent is a payload-less liveness beat the server sends on a timer.
// Clients use its arrival (and the silence when it stops) to track connectivity.
func NewHeartbeatEvent() *Event {
	return &Event{Type: "heartbeat", Timestamp: time.Now().UnixNano()}
}

type DefinitionSyncPayload struct {
	Kind    string `json:"kind"`
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
}
type AlertPayload struct {
	RuleID       string            `json:"ruleId"`
	RuleName     string            `json:"ruleName"`
	Severity     string            `json:"severity"`
	State        string            `json:"state"`
	Transition   string            `json:"transition"`
	GroupLabels  map[string]string `json:"groupLabels"`
	CurrentValue *float64          `json:"currentValue,omitempty"`
	Threshold    *float64          `json:"threshold,omitempty"`
	Operator     string            `json:"operator,omitempty"`
	Link         string            `json:"link,omitempty"`
	Browser      bool              `json:"browser,omitempty"`
}
type AlertSyncPayload struct {
	RuleID string `json:"ruleId,omitempty"`
	Reason string `json:"reason"`
	Events any    `json:"events,omitempty"`
	Total  *int   `json:"total,omitempty"`
}
type NotificationPayload struct {
	ID       string `json:"id"`
	Source   string `json:"source"`
	SourceID string `json:"sourceId"`
}
type ResourcesChangedPayload struct {
	Resources []Resource `json:"resources"`
	Reason    string     `json:"reason"`
	SessionID string     `json:"sessionId,omitempty"`
}

func NewResourcesChangedEvent(resources []Resource, reason, sessionID string) *Event {
	return &Event{Type: "resources_changed", Timestamp: time.Now().UnixNano(), Payload: &ResourcesChangedPayload{resources, reason, sessionID}}
}

type MetricRowPayload struct {
	Name                   string   `json:"name"`
	Description            string   `json:"description"`
	Unit                   string   `json:"unit"`
	Type                   string   `json:"type"`
	AggregationTemporality string   `json:"aggregation_temporality,omitempty"`
	IsMonotonic            *bool    `json:"is_monotonic,omitempty"`
	StartTimestampNs       int64    `json:"start_timestamp_ns,omitempty"`
	TimestampNs            int64    `json:"timestamp_ns"`
	Flags                  uint32   `json:"flags,omitempty"`
	Value                  float64  `json:"value,omitempty"`
	HistogramCount         *uint64  `json:"histogram_count,omitempty"`
	HistogramSum           *float64 `json:"histogram_sum,omitempty"`
	HistogramMin           *float64 `json:"histogram_min,omitempty"`
	HistogramMax           *float64 `json:"histogram_max,omitempty"`
	ExplicitBounds         string   `json:"explicit_bounds,omitempty"`
	BucketCounts           string   `json:"bucket_counts,omitempty"`
	ExpScale               *int32   `json:"exp_scale,omitempty"`
	ExpZeroCount           *uint64  `json:"exp_zero_count,omitempty"`
	ExpZeroThreshold       *float64 `json:"exp_zero_threshold,omitempty"`
	ExpPositiveOffset      *int32   `json:"exp_positive_offset,omitempty"`
	ExpPositiveCounts      string   `json:"exp_positive_counts,omitempty"`
	ExpNegativeOffset      *int32   `json:"exp_negative_offset,omitempty"`
	ExpNegativeCounts      string   `json:"exp_negative_counts,omitempty"`
	SummaryCount           *uint64  `json:"summary_count,omitempty"`
	SummarySum             *float64 `json:"summary_sum,omitempty"`
	SummaryQuantiles       string   `json:"summary_quantiles,omitempty"`
	Attributes             string   `json:"attributes"`
	Resource               string   `json:"resource"`
	SeriesAttributes       string   `json:"series_attributes"`
	SeriesKey              string   `json:"series_key"`
	ScopeName              string   `json:"scope_name"`
	ScopeVersion           string   `json:"scope_version"`
	ScopeSchemaURL         string   `json:"scope_schema_url"`
	ScopeAttributes        string   `json:"scope_attributes"`
	Exemplars              string   `json:"exemplars"`
	ServiceName            string   `json:"service_name"`
	SessionID              string   `json:"session_id"`
}

// UnmarshalJSON gives Go consumers a precise typed metric row while allowing
// server publishers to pass their existing storage DTO into the Row field.
func (p *MetricPayload) UnmarshalJSON(data []byte) error {
	var raw struct {
		Name        string          `json:"name"`
		ServiceName string          `json:"serviceName"`
		Value       float64         `json:"value"`
		Type        string          `json:"type"`
		CatalogOnly bool            `json:"catalogOnly"`
		Row         json.RawMessage `json:"row"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	*p = MetricPayload{Name: raw.Name, ServiceName: raw.ServiceName, Value: raw.Value, Type: raw.Type, CatalogOnly: raw.CatalogOnly}
	if len(raw.Row) > 0 && string(raw.Row) != "null" {
		var row MetricRowPayload
		if err := json.Unmarshal(raw.Row, &row); err != nil {
			return err
		}
		p.Row = &row
	}
	return nil
}

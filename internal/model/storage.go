// Package model owns the persisted Spaniel schema types. Keeping these types
// outside storage lets generated GORM DAOs depend on the schema without an
// import cycle back into the storage service layer.
package model

// SearchResult is a read projection shared by generated cross-table search
// methods and the storage API. It is not a persisted table model.
type SearchResult struct {
	Kind      string `json:"kind"`
	TraceID   string `json:"trace_id"`
	SpanID    string `json:"span_id,omitempty"`
	Title     string `json:"title"`
	Subtitle  string `json:"subtitle"`
	SessionID string `json:"session_id"`
}

// NPlusOneSearchResult and LintSearchResult are generated-query projections.
type NPlusOneSearchResult struct {
	TraceID   string
	Title     string
	Count     int
	SessionID string
}

type LintSearchResult struct {
	TraceID   string
	Title     string
	RuleID    string
	SessionID string
}

// TraceOverlay is the lightweight chart marker projection.
type TraceOverlay struct {
	TraceID    string `json:"trace_id"`
	Op         string `json:"op"`
	Service    string `json:"service"`
	StatusCode int    `json:"status_code"`
	StartNs    int64  `json:"start_ns"`
	EndNs      int64  `json:"end_ns"`
	DurationNs int64  `json:"duration_ns"`
}

// SpanRow is the flat span-list projection with its derived UI tag.
type SpanRow struct {
	Span
	Tag string `json:"tag,omitempty"`
}

// SpanGroup is the operation-level aggregate used by grouped span views.
type SpanGroup struct {
	ServiceName       string `json:"service_name"`
	Name              string `json:"name"`
	Kind              int    `json:"kind"`
	Count             int    `json:"count"`
	LatestStartNs     int64  `json:"latest_start_ns"`
	ErrorCount        int    `json:"error_count"`
	P50DurationNs     int64  `json:"p50_duration_ns"`
	P95DurationNs     int64  `json:"p95_duration_ns"`
	MaxDurationNs     int64  `json:"max_duration_ns"`
	AttributeVariants int    `json:"attribute_variants"`
}

// TraceListRow is the generated query projection before storage parses the
// comma-delimited issue kinds for the API response.
type TraceListRow struct {
	TraceID       string
	ServiceName   string
	Name          string
	Attributes    string
	StatusCode    int
	StartNs       int64
	EndNs         int64
	DurationNs    int64
	SessionID     string
	SessionLabel  string
	HasN1         bool
	SpanCount     int
	IssueKindsRaw string
}

type CountValue struct{ Count int64 }

// StatsRow is the aggregate projection used by the generated span statistics query.
type StatsRow struct {
	SpanCount       int64
	TraceCount      int64
	LogCount        int64
	SessionCount    int64
	OldestSessionAt int64
}

type SourceStatsRow struct {
	ServiceName string
	SpanCount   int64
	ErrorCount  int64
	BytesTotal  int64
	FirstSeen   int64
	LastSeen    int64
}

type SessionSummary struct {
	ID             string
	Label          string
	CreatedAt      int64
	IsBaseline     bool
	IsImported     bool
	SpanCount      int
	Services       string
	Note           string
	LastActivityNs int64
	TraceCount     int
	P95Ns          int64
	SizeBytes      int64
	N1Count        int
	ErrorCount     int
}

type MetricCatalogEntry struct {
	Name                   string `json:"name"`
	Description            string `json:"description"`
	Unit                   string `json:"unit"`
	Type                   string `json:"type"`
	AggregationTemporality string `json:"aggregation_temporality,omitempty"`
	IsMonotonic            *bool  `json:"is_monotonic,omitempty"`
	ServiceName            string `json:"service_name"`
	SampleCount            int    `json:"sample_count"`
	LastTimestampNs        int64  `json:"last_timestamp_ns"`
}

// MetricSeriesCatalog is the durable identity set admitted by the cardinality
// policy. It has no user-facing representation beyond aggregate counts.
type MetricSeriesCatalog struct {
	SessionID        string `gorm:"primaryKey"`
	ServiceName      string `gorm:"primaryKey"`
	Name             string `gorm:"primaryKey"`
	SeriesKey        string `gorm:"primaryKey"`
	SeriesAttributes string
	FirstTimestampNs int64
	LastTimestampNs  int64
	PointCount       int64
}

func (MetricSeriesCatalog) TableName() string { return "metric_series_catalog" }

type P95Value struct{ P95Ns int64 }

// Meta is the persisted key-value table for small storage state such as drop
// counters and the running Spaniel version.
type Meta struct {
	Key   string `gorm:"column:meta_key;primaryKey"`
	Value string `gorm:"column:meta_value"`
}

func (Meta) TableName() string { return "meta" }

type ServiceMapOpStat struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
	P95Ns int64  `json:"p95_ns"`
}

type ServiceMapNode struct {
	ID         string             `json:"id"`
	SpanCount  int                `json:"span_count"`
	ErrorCount int                `json:"error_count"`
	P95Ns      int64              `json:"p95_ns"`
	TopOps     []ServiceMapOpStat `json:"top_operations" gorm:"-"`
}

type ServiceMapEdge struct {
	From          string `json:"from"`
	To            string `json:"to"`
	CallCount     int    `json:"call_count"`
	AvgDurationNs int64  `json:"avg_duration_ns"`
	ErrorCount    int    `json:"error_count"`
}

type TableSizeRow struct {
	Name         string
	RowCount     int64
	PayloadBytes int64
}

type DatabaseSize struct {
	BlockSize  int64
	UsedBlocks int64
}

type SessionSize struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	ApproxBytes int64  `json:"approx_bytes"`
	SpanCount   int    `json:"span_count"`
}

type Span struct {
	TraceID       string       `json:"trace_id"`
	SpanID        string       `json:"span_id"`
	ParentSpanID  string       `json:"parent_span_id"`
	ServiceName   string       `json:"service_name"`
	Name          string       `json:"name"`
	Kind          int          `json:"kind"`
	StartNs       int64        `json:"start_ns"`
	EndNs         int64        `json:"end_ns"`
	DurationNs    int64        `json:"duration_ns"`
	StatusCode    int          `json:"status_code"`
	StatusMessage string       `json:"status_message"`
	Attributes    string       `json:"attributes"`
	Resource      string       `json:"resource"`
	SessionID     string       `json:"session_id"`
	SessionLabel  string       `json:"session_label"`
	ReceivedAt    int64        `json:"received_at"`
	Sampled       bool         `json:"sampled"`
	Events        []*SpanEvent `json:"events" gorm:"-"`
	Links         []*SpanLink  `json:"links" gorm:"-"`
}

func (Span) TableName() string { return "spans" }

type Log struct {
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

func (Log) TableName() string { return "logs" }

type Session struct {
	ID             string `json:"id" gorm:"primaryKey"`
	Label          string `json:"label"`
	CreatedAt      int64  `json:"created_at"`
	IsBaseline     bool   `json:"is_baseline"`
	IsImported     bool   `json:"is_imported"`
	SpanCount      int    `json:"span_count"`
	TraceCount     int    `json:"trace_count" gorm:"-"`
	Services       string `json:"services"`
	Note           string `json:"note"`
	LastActivityNs int64  `json:"last_activity_ns"`
	P95Ns          int64  `json:"p95_ns" gorm:"-"`
	SizeBytes      int64  `json:"size_bytes" gorm:"-"`
	N1Count        int    `json:"n1_count" gorm:"-"`
	ErrorCount     int    `json:"error_count" gorm:"-"`
}

func (Session) TableName() string { return "sessions" }

type LintWarning struct {
	SpanID    string `json:"span_id"`
	TraceID   string `json:"trace_id"`
	SessionID string `json:"session_id"`
	RuleID    string `json:"rule_id"`
	Message   string `json:"message"`
	Severity  string `json:"severity"`
	CreatedAt int64  `json:"created_at"`
}

func (LintWarning) TableName() string { return "lint_warnings" }

type TraceIssue struct {
	ID            string `json:"id" gorm:"primaryKey"`
	TraceID       string `json:"trace_id"`
	SessionID     string `json:"session_id"`
	Kind          string `json:"kind"`
	Fingerprint   string `json:"fingerprint"`
	Count         int    `json:"count"`
	WastedNs      int64  `json:"wasted_ns"`
	ParentSpanID  string `json:"parent_span_id"`
	ExampleSpanID string `json:"example_span_id"`
	CreatedAt     int64  `json:"created_at"`
}

func (TraceIssue) TableName() string { return "trace_issues" }

type SpanEvent struct {
	SpanID     string `json:"span_id"`
	TraceID    string `json:"trace_id"`
	SessionID  string `json:"session_id"`
	TimeNs     int64  `json:"time_ns"`
	Name       string `json:"name"`
	Attributes string `json:"attributes"`
}

func (SpanEvent) TableName() string { return "span_events" }

type SpanLink struct {
	SpanID        string `json:"span_id"`
	TraceID       string `json:"trace_id"`
	SessionID     string `json:"session_id"`
	LinkedTraceID string `json:"linked_trace_id"`
	LinkedSpanID  string `json:"linked_span_id"`
	TraceState    string `json:"trace_state"`
	Attributes    string `json:"attributes"`
}

func (SpanLink) TableName() string { return "span_links" }

type Metric struct {
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
	SeriesNew              bool     `json:"-" gorm:"-"`
}

func (Metric) TableName() string { return "metrics" }

type Dashboard struct {
	ID          string               `json:"id"`
	Name        string               `json:"name"`
	Description string               `json:"description"`
	CreatedAt   int64                `json:"created_at"`
	UpdatedAt   int64                `json:"updated_at"`
	Variables   []*DashboardVariable `json:"variables"`
	Panels      []*DashboardPanel    `json:"panels"`
}

func (Dashboard) TableName() string { return "dashboards" }

type DashboardVariable struct {
	DashboardID  string `json:"dashboard_id"`
	Name         string `json:"name"`
	Kind         string `json:"kind"`
	Source       string `json:"source"`
	OptionsJSON  string `json:"options_json"`
	DefaultValue string `json:"default_value"`
}

func (DashboardVariable) TableName() string { return "dashboard_variables" }

type DashboardPanel struct {
	ID           string `json:"id"`
	DashboardID  string `json:"dashboard_id"`
	Title        string `json:"title"`
	DisplayType  string `json:"display_type"`
	QuerySQL     string `json:"query_sql"`
	QueryVersion int    `json:"query_version"`
	SettingsJSON string `json:"settings_json"`
	LayoutJSON   string `json:"layout_json"`
	Position     int    `json:"position"`
	UpdatedAt    int64  `json:"updated_at"`
}

func (DashboardPanel) TableName() string { return "dashboard_panels" }

type AlertRule struct {
	ID                            string           `json:"id"`
	Name                          string           `json:"name"`
	QuerySQL                      string           `json:"query_sql"`
	QueryVersion                  int              `json:"query_version"`
	ConditionJSON                 string           `json:"condition_json"`
	GroupByJSON                   string           `json:"group_by_json"`
	PendingForNs                  int64            `json:"pending_for_ns"`
	CooldownNs                    int64            `json:"cooldown_ns"`
	RepeatIntervalNs              int64            `json:"repeat_interval_ns"`
	Severity                      string           `json:"severity"`
	AnnotationsJSON               string           `json:"annotations_json"`
	Enabled                       bool             `json:"enabled"`
	BrowserEnabled                bool             `json:"browser_enabled"`
	PushoverEnabled               bool             `json:"pushover_enabled"`
	InstanceDiscoverySQL          string           `json:"instance_discovery_sql"`
	InstanceDiscoveryIntervalNs   int64            `json:"instance_discovery_interval_ns"`
	InstanceDiscoveryStaleAfterNs int64            `json:"instance_discovery_stale_after_ns"`
	InstanceDiscoveryLastRunAt    int64            `json:"instance_discovery_last_run_at"`
	LastEvaluatedAt               int64            `json:"last_evaluated_at"`
	LastSuccessAt                 int64            `json:"last_success_at"`
	LastDurationNs                int64            `json:"last_duration_ns"`
	NextEvaluationAt              int64            `json:"next_evaluation_at"`
	LastError                     string           `json:"last_error"`
	SourceFile                    string           `json:"source_file"`
	SourceHash                    string           `json:"source_hash"`
	CreatedAt                     int64            `json:"created_at" gorm:"autoCreateTime:nano"`
	UpdatedAt                     int64            `json:"updated_at" gorm:"autoUpdateTime:nano"`
	Instances                     []*AlertInstance `json:"instances,omitempty" gorm:"-"`
}

func (AlertRule) TableName() string { return "alert_rules" }

// AlertInstanceTarget is a discovered member of an alert's expected instance
// universe. It makes an absent group observable without requiring alert SQL to
// embed a static VALUES list, while preserving a bounded stale-target window.
type AlertInstanceTarget struct {
	RuleID       string `json:"rule_id" gorm:"primaryKey"`
	GroupKey     string `json:"group_key" gorm:"primaryKey"`
	LabelsJSON   string `json:"labels_json"`
	DiscoveredAt int64  `json:"discovered_at"`
	LastSeenAt   int64  `json:"last_seen_at"`
}

func (AlertInstanceTarget) TableName() string { return "alert_instance_targets" }

type AlertInstance struct {
	RuleID              string   `json:"rule_id" gorm:"primaryKey"`
	GroupKey            string   `json:"group_key" gorm:"primaryKey"`
	LabelsJSON          string   `json:"labels_json"`
	State               string   `json:"state"`
	Value               *float64 `json:"value"`
	FirstPendingAt      *int64   `json:"first_pending_at"`
	FiredAt             *int64   `json:"fired_at"`
	ResolvedAt          *int64   `json:"resolved_at"`
	AcknowledgedAt      *int64   `json:"acknowledged_at"`
	AcknowledgementNote string   `json:"acknowledgement_note"`
	LastEvaluatedAt     int64    `json:"last_evaluated_at"`
	LastError           string   `json:"last_error"`
	LastNotifiedAt      *int64   `json:"last_notified_at"`
}

func (AlertInstance) TableName() string { return "alert_instances" }

// AlertEvent is an append-only account of evaluation, lifecycle, and delivery
// activity. It deliberately complements AlertInstance, which is only the
// current state for a rule/group.
type AlertEvent struct {
	ID        string   `json:"id"`
	RuleID    string   `json:"rule_id"`
	GroupKey  string   `json:"group_key"`
	Kind      string   `json:"kind"`
	State     string   `json:"state"`
	Value     *float64 `json:"value"`
	Detail    string   `json:"detail"`
	CreatedAt int64    `json:"created_at"`
}

func (AlertEvent) TableName() string { return "alert_events" }

type AlertSilence struct {
	ID        string `json:"id"`
	RuleID    string `json:"rule_id"`
	GroupKey  string `json:"group_key"`
	Comment   string `json:"comment"`
	StartsAt  int64  `json:"starts_at"`
	EndsAt    int64  `json:"ends_at"`
	CreatedAt int64  `json:"created_at"`
}

func (AlertSilence) TableName() string { return "alert_silences" }

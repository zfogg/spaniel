package liveevent

import (
	"encoding/json"
	"fmt"
	"reflect"
)

// Resource values are also browser query-key prefixes. Details and lists are
// separate resources so consumers can refresh only subscribed views.
type Resource string

var TelemetryResources = []Resource{"traces", "trace", "spans", "span", "logs", "metrics", "metric-series", "lint", "issues", "dashboard-panel", "alerts", "sessions", "stats", "sources", "services", "service-map", "coverage"}
var AllResources = append(append([]Resource{}, TelemetryResources...), "dashboards", "active-session", "notifications")

// Invalidations is the shared refresh matrix. self_* and catalogOnly never
// cause HTTP reads: the complete rows are cache patches, preventing feedback.
var Invalidations = map[string][]Resource{
	"span":           {"traces", "trace", "spans", "span", "services", "service-map", "coverage", "sessions", "dashboard-panel"},
	"log":            {"logs", "services", "dashboard-panel"},
	"metric":         {"metrics", "metric-series", "services", "dashboard-panel"},
	"issue":          {"issues", "trace"},
	"alert":          {"alerts", "notifications"},
	"alert_sync":     {"alerts"},
	"notification":   {"notifications"},
	"active_session": TelemetryResources,
}

// Patches names the resources updated directly from row payloads. It also
// lets clients replay rows that arrive during an authoritative HTTP snapshot.
var Patches = map[string][]Resource{
	"self_trace": {"traces"}, "self_span": {"spans", "span", "trace"},
	"self_log": {"logs"}, "metric": {"metrics", "metric-series"},
}

func Affects(m Message) []Resource {
	return append(append([]Resource{}, Changes(m)...), Patches[m.Type]...)
}

// Payloads is the canonical event registry, used by Decode and genliveevent.
var Payloads = map[string]reflect.Type{
	"span": reflect.TypeFor[SpanPayload](), "log": reflect.TypeFor[LogPayload](),
	"metric": reflect.TypeFor[MetricPayload](), "issue": reflect.TypeFor[IssuePayload](),
	"self_trace": reflect.TypeFor[SelfTracePayload](), "self_span": reflect.TypeFor[SelfSpanPayload](), "self_log": reflect.TypeFor[SelfLogPayload](),
	"forwarder": reflect.TypeFor[ForwarderPayload](), "throughput": reflect.TypeFor[ThroughputPayload](),
	"live_state": reflect.TypeFor[LiveStatePayload](), "active_session": reflect.TypeFor[ActiveSessionPayload](),
	"definition_sync": reflect.TypeFor[DefinitionSyncPayload](), "alert": reflect.TypeFor[AlertPayload](),
	"alert_sync": reflect.TypeFor[AlertSyncPayload](), "notification": reflect.TypeFor[NotificationPayload](),
	"resources_changed": reflect.TypeFor[ResourcesChangedPayload](), "heartbeat": nil,
}

// Message retains raw bytes for consumers needing HTTP DTO precision, alongside
// a typed Payload for safe dispatch. Unknown events are valid and ignored.
type Message struct {
	Type      string          `json:"type"`
	Timestamp int64           `json:"timestamp_ns"`
	Payload   json.RawMessage `json:"payload"`
	Decoded   any             `json:"-"`
}

func Decode(data []byte) (Message, error) {
	var m Message
	if err := json.Unmarshal(data, &m); err != nil {
		return m, err
	}
	if m.Type == "" {
		return m, fmt.Errorf("missing event type")
	}
	t, known := Payloads[m.Type]
	if !known || t == nil {
		return m, nil
	}
	if len(m.Payload) == 0 || string(m.Payload) == "null" {
		return m, fmt.Errorf("missing %s payload", m.Type)
	}
	v := reflect.New(t).Interface()
	if err := json.Unmarshal(m.Payload, v); err != nil {
		return m, err
	}
	m.Decoded = v
	return m, nil
}

// ResourceAliases include browser-specific cache keys for the same resource.
// Both clients use the expanded matrix; irrelevant prefixes are simply ignored.
var ResourceAliases = map[Resource][]Resource{
	"span": {"trace-span"}, "trace": {"trace-incoming-links"},
	"dashboard-panel": {"dashboard-annotations", "dashboard-runtime"},
}

func Changes(m Message) []Resource {
	resources := changes(m)
	out := append([]Resource{}, resources...)
	for _, r := range resources {
		out = append(out, ResourceAliases[r]...)
	}
	return out
}
func changes(m Message) []Resource {
	if m.Type == "metric" {
		if p, ok := m.Decoded.(*MetricPayload); ok && p.CatalogOnly {
			return nil
		}
	}
	if p, ok := m.Decoded.(*ResourcesChangedPayload); ok {
		return p.Resources
	}
	if p, ok := m.Decoded.(*DefinitionSyncPayload); ok {
		if p.Status != "updated" {
			return nil
		}
		if p.Kind == "dashboard" {
			return []Resource{"dashboards", "dashboard-panel"}
		}
		if p.Kind == "alert" {
			return []Resource{"alerts"}
		}
	}
	return Invalidations[m.Type]
}

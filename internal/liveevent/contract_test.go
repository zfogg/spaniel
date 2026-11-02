package liveevent

import (
	"encoding/json"
	"slices"
	"testing"
)

func TestDecodeAndResourcePolicy(t *testing.T) {
	for _, bad := range []string{`{`, `{}`, `{"type":"span","payload":null}`, `{"type":"span","payload":{"traceId":9}}`} {
		if _, err := Decode([]byte(bad)); err == nil {
			t.Fatalf("accepted malformed event %s", bad)
		}
	}
	unknown, err := Decode([]byte(`{"type":"future","payload":{}}`))
	if err != nil || unknown.Decoded != nil {
		t.Fatal("unknown event broke forward compatibility")
	}
	tests := []struct {
		event *Event
		want  Resource
		empty bool
	}{
		{NewSpanEvent(&SpanPayload{}), "traces", false}, {NewSelfSpanEvent(&SelfSpanPayload{}), "", true}, {NewMetricEvent(&MetricPayload{CatalogOnly: true}), "", true},
		{NewResourcesChangedEvent([]Resource{"lint"}, "persisted", "s"), "lint", false},
		{&Event{Type: "definition_sync", Payload: DefinitionSyncPayload{Kind: "dashboard", Status: "updated"}}, "dashboards", false},
		{&Event{Type: "definition_sync", Payload: DefinitionSyncPayload{Kind: "dashboard", Status: "updating"}}, "", true},
	}
	for _, tc := range tests {
		b, _ := json.Marshal(tc.event)
		m, err := Decode(b)
		if err != nil {
			t.Fatal(err)
		}
		changes := Changes(m)
		if tc.empty && len(changes) > 0 || !tc.empty && !slices.Contains(changes, tc.want) {
			t.Fatalf("wrong %s policy: %v", m.Type, changes)
		}
	}
}

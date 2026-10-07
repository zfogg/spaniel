// Command annotateopenapi adds deterministic navigation metadata to the API contract.
package main

import (
	"encoding/json"
	"os"
	"sort"
	"strings"
)

type operation map[string]any
type document struct { Tags []map[string]string `json:"tags"`; Paths map[string]map[string]operation `json:"paths"` }

func main() {
	b, err := os.ReadFile("api/openapi.json"); if err != nil { panic(err) }
	var d document
	if err := json.Unmarshal(b, &d); err != nil { panic(err) }
	d.Tags = []map[string]string{
		{"name":"Traces", "description":"Trace, span, log, and service investigation."},
		{"name":"Sessions", "description":"Capture-session lifecycle and comparisons."},
		{"name":"Metrics", "description":"Metric stream discovery and time series."},
		{"name":"Dashboards", "description":"Saved dashboard definitions and panels."},
		{"name":"Alerts", "description":"Alert rules, history, acknowledgements, and silences."},
		{"name":"Administration", "description":"Settings, storage, sources, and system metadata."},
	}
	for path, methods := range d.Paths { for _, op := range methods { op["tags"] = []string{tag(path)} } }
	out, err := json.MarshalIndent(d, "", "  "); if err != nil { panic(err) }
	if err := os.WriteFile("api/openapi.json", append(out, '\n'), 0o644); err != nil { panic(err) }
}

func tag(path string) string {
	switch {
	case strings.HasPrefix(path, "/api/traces"), strings.HasPrefix(path, "/api/spans"), strings.HasPrefix(path, "/api/logs"), strings.HasPrefix(path, "/api/services"), strings.HasPrefix(path, "/api/lint"), strings.HasPrefix(path, "/api/issues"), strings.HasPrefix(path, "/api/diff"), strings.HasPrefix(path, "/api/search"): return "Traces"
	case strings.HasPrefix(path, "/api/sessions"): return "Sessions"
	case strings.HasPrefix(path, "/api/metrics"): return "Metrics"
	case strings.HasPrefix(path, "/api/dashboards"): return "Dashboards"
	case strings.HasPrefix(path, "/api/alerts"): return "Alerts"
	default: return "Administration"
	}
}

var _ = sort.Strings

// Command enrichopenapi applies documentation metadata without changing API semantics.
package main

import (
	"encoding/json"
	"os"
	"strings"
)

func main() {
	b, err := os.ReadFile("api/openapi.json")
	if err != nil {
		panic(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(b, &doc); err != nil {
		panic(err)
	}
	doc["tags"] = []any{map[string]any{"name": "Traces", "description": "Trace, span, log, service, and investigation APIs."}, map[string]any{"name": "Sessions", "description": "Capture session lifecycle and comparison APIs."}, map[string]any{"name": "Metrics", "description": "Metric catalog, cardinality, and time-series APIs."}, map[string]any{"name": "Dashboards", "description": "Dashboard definitions, panels, and variables."}, map[string]any{"name": "Alerts", "description": "Alert rules, instances, events, and silences."}, map[string]any{"name": "Administration", "description": "Settings, storage, coverage, schema, and source metadata."}}
	for path, item := range doc["paths"].(map[string]any) {
		for method, raw := range item.(map[string]any) {
			if method != "parameters" {
				op := raw.(map[string]any)
				op["tags"] = []any{tag(path)}
				addExample(op, path)
			}
		}
	}
	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		panic(err)
	}
	if err := os.WriteFile("api/openapi.json", append(out, '\n'), 0o644); err != nil {
		panic(err)
	}
}
func addExample(op map[string]any, path string) {
	responses, _ := op["responses"].(map[string]any)
	ok, _ := responses["200"].(map[string]any)
	content, _ := ok["content"].(map[string]any)
	jsonContent, _ := content["application/json"].(map[string]any)
	if jsonContent != nil {
		jsonContent["examples"] = map[string]any{"example": map[string]any{"summary": "Representative Spaniel response", "value": envelope(path)}}
	}
}
func tag(path string) string {
	switch {
	case strings.HasPrefix(path, "/api/alerts"):
		return "Alerts"
	case strings.HasPrefix(path, "/api/dashboards"):
		return "Dashboards"
	case strings.HasPrefix(path, "/api/metrics"):
		return "Metrics"
	case strings.HasPrefix(path, "/api/sessions"):
		return "Sessions"
	case strings.HasPrefix(path, "/api/traces"), strings.HasPrefix(path, "/api/spans"), strings.HasPrefix(path, "/api/logs"), strings.HasPrefix(path, "/api/services"), strings.HasPrefix(path, "/api/lint"), strings.HasPrefix(path, "/api/issues"), strings.HasPrefix(path, "/api/search"), strings.HasPrefix(path, "/api/diff"):
		return "Traces"
	default:
		return "Administration"
	}
}
func envelope(path string) map[string]any {
	return map[string]any{"data": example(path), "meta": map[string]any{"total": 1, "page": 1}}
}
func example(path string) any {
	switch {
	case strings.Contains(path, "/traces"):
		return []any{map[string]any{"trace_id": "6d8f4a9c2e1b7f30", "service_name": "checkout", "name": "POST /checkout", "duration_ns": 182000000, "status_code": 1}}
	case strings.Contains(path, "/spans"):
		return []any{map[string]any{"span_id": "19ab3f8e", "trace_id": "6d8f4a9c2e1b7f30", "name": "SELECT orders", "service_name": "postgres", "duration_ns": 12000000}}
	case strings.Contains(path, "/logs"):
		return []any{map[string]any{"timestamp_ns": 1730000000000000000, "severity": "ERROR", "body": "payment authorization failed", "service_name": "checkout"}}
	case strings.Contains(path, "/metrics"):
		return []any{map[string]any{"name": "http.server.duration", "service_name": "checkout", "unit": "ms"}}
	case strings.Contains(path, "/sessions"):
		return []any{map[string]any{"id": "session_2026-10-07_1200", "label": "checkout regression", "is_baseline": false}}
	case strings.Contains(path, "/dashboards"):
		return []any{map[string]any{"id": "service-overview", "name": "Service overview", "panels": []any{}}}
	case strings.Contains(path, "/alerts"):
		return []any{map[string]any{"id": "slow-checkout", "name": "Slow checkout", "enabled": true, "severity": "warning"}}
	case strings.Contains(path, "/health"):
		return map[string]any{"ok": true}
	case strings.Contains(path, "/settings"):
		return map[string]any{"http_port": 8080, "retention_days": 7}
	case strings.Contains(path, "/coverage"):
		return map[string]any{"covered_routes": 24, "total_routes": 31, "coverage_percent": 77.4}
	default:
		return map[string]any{"ok": true}
	}
}

// Command enrichopenapi applies documentation metadata without changing API semantics.
package main

import (
	"encoding/json"
	"os"
	"sort"
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
	applyFrontendSchemas(doc)
	applyRequestContracts(doc)
	doc["tags"] = []any{map[string]any{"name": "Traces", "description": "Trace, span, log, service, and investigation APIs."}, map[string]any{"name": "Sessions", "description": "Capture session lifecycle and comparison APIs."}, map[string]any{"name": "Metrics", "description": "Metric catalog, cardinality, and time-series APIs."}, map[string]any{"name": "Dashboards", "description": "Dashboard definitions, panels, and variables."}, map[string]any{"name": "Alerts", "description": "Alert rules, instances, events, and silences."}, map[string]any{"name": "Administration", "description": "Settings, storage, coverage, schema, and source metadata."}}
	for path, item := range doc["paths"].(map[string]any) {
		for method, raw := range item.(map[string]any) {
			if method != "parameters" {
				op := raw.(map[string]any)
				op["tags"] = []any{tag(path)}
				applyResponseSchema(op)
				applyExportSchema(op)
				addExample(op, doc)
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

func applyExportSchema(op map[string]any) {
	operationID, _ := op["operationId"].(string)
	mediaType, ok := exportMediaTypes[operationID]
	if !ok {
		return
	}
	responses := op["responses"].(map[string]any)
	response := responses["200"].(map[string]any)
	response["content"] = map[string]any{mediaType: map[string]any{"schema": map[string]any{"type": "string"}}}
}

var exportMediaTypes = map[string]string{
	"exportAlertConfig":     "application/yaml",
	"exportDashboardConfig": "application/yaml",
	"exportSessionBaseline": "application/json",
	"exportTrace":           "application/json",
	"getOpenAPISpec":        "application/vnd.oai.openapi+json;version=3.1",
}

// applyFrontendSchemas imports the response shapes that already protect the
// browser at runtime. They become named OpenAPI components so generated client
// code can use the same contract at compile time.
func applyFrontendSchemas(doc map[string]any) {
	b, err := os.ReadFile("api/frontend-schemas.json")
	if err != nil {
		panic(err)
	}
	var schemas map[string]any
	if err := json.Unmarshal(b, &schemas); err != nil {
		panic(err)
	}
	components := doc["components"].(map[string]any)
	componentSchemas := components["schemas"].(map[string]any)
	for name, schema := range schemas {
		componentSchemas[name] = schema
	}
	componentSchemas["Ok"] = map[string]any{"type": "object", "required": []any{"ok"}, "properties": map[string]any{"ok": map[string]any{"type": "boolean"}}}
	componentSchemas["String"] = map[string]any{"type": "string"}
	componentSchemas["ActiveSession"] = map[string]any{"type": "object", "required": []any{"id", "label"}, "properties": map[string]any{"id": map[string]any{"type": "string"}, "label": map[string]any{"type": "string"}}}
	componentSchemas["AlertTestNotification"] = map[string]any{"type": "object", "required": []any{"destination", "status"}, "properties": map[string]any{"destination": map[string]any{"type": "string", "enum": []any{"browser", "pushover"}}, "status": map[string]any{"type": "string", "enum": []any{"sent", "suppressed"}}, "body": map[string]any{"type": "string"}}}
}

func applyResponseSchema(op map[string]any) {
	operationID, _ := op["operationId"].(string)
	name, ok := responseSchemas[operationID]
	if !ok {
		return
	}
	responses := op["responses"].(map[string]any)
	response, _ := responses["200"].(map[string]any)
	content, _ := response["content"].(map[string]any)
	jsonContent, _ := content["application/json"].(map[string]any)
	if jsonContent == nil {
		return
	}
	dataSchema := map[string]any{"$ref": "#/components/schemas/" + name}
	if strings.HasPrefix(name, "[]") {
		dataSchema = map[string]any{"type": "array", "items": map[string]any{"$ref": "#/components/schemas/" + strings.TrimPrefix(name, "[]")}}
	}
	jsonContent["schema"] = map[string]any{"type": "object", "required": []any{"data", "meta"}, "properties": map[string]any{"data": dataSchema, "meta": map[string]any{"$ref": "#/components/schemas/Meta"}}}
}

var responseSchemas = map[string]string{
	"listAlerts": "AlertList", "createAlert": "AlertRule", "listAlertHistory": "[]AlertEvent", "importAlertConfig": "AlertRule", "previewAlertDraft": "QueryPreview", "reloadAlertDefinitions": "Ok", "deleteAlert": "Ok", "getAlert": "AlertRule", "patchAlert": "AlertRule", "acknowledgeAlert": "Ok", "duplicateAlert": "AlertRule", "listAlertEvents": "[]AlertEvent", "acknowledgeAlertInstance": "AlertInstance", "unacknowledgeAlertInstance": "AlertInstance", "previewAlert": "QueryPreview", "listAlertSilences": "[]AlertSilence", "createAlertSilence": "AlertSilence", "deleteAlertSilence": "Ok", "patchAlertSilence": "AlertSilence", "testAlertNotification": "AlertTestNotification",
	"getCoverage": "CoverageReport", "listDashboards": "[]Dashboard", "createDashboard": "Dashboard", "importDashboardConfig": "Dashboard", "reorderDashboards": "Ok", "deleteDashboard": "Ok", "getDashboard": "Dashboard", "patchDashboard": "Dashboard", "createDashboardPanel": "DashboardPanel", "deleteDashboardPanel": "Ok", "patchDashboardPanel": "DashboardPanel", "moveDashboardPanel": "Ok", "previewDashboardQuery": "QueryPreview", "createDashboardVariable": "DashboardVariable", "deleteDashboardVariable": "Ok",
	"getDatabaseSchema": "DatabaseSchemaCatalog", "getDiff": "DiffResult", "listForwarders": "[]ForwarderStatus", "getHealth": "Ok", "listIssues": "[]TraceIssue", "listLint": "[]LintWarning", "listLogs": "[]Log", "listMetrics": "[]MetricCatalogEntry", "getMetricCardinality": "[]MetricCardinalityStream", "getMetricSeries": "MetricSeries", "listQueryCatalog": "[]QueryCatalogEntry", "searchTelemetry": "[]SearchResult", "getServiceMap": "ServiceMapData", "listServices": "[]String", "listSessions": "[]Session", "createSession": "Session", "getActiveSession": "ActiveSession", "importSession": "ImportResult", "deleteSession": "Ok", "getSession": "Session", "patchSession": "Session", "activateSession": "Session", "setSessionBaseline": "Ok", "getSettings": "SettingsResponse", "putSettings": "Settings", "checkUpdates": "UpdateCheckResult", "compactStorage": "CompactResult", "dropAllData": "Ok", "pruneStorage": "PruneResult", "listSources": "[]SourceStats", "listSpans": "[]SpanRow", "getSpan": "Span", "getStats": "Stats", "getStorageBreakdown": "StorageBreakdown", "listTraces": "[]TraceRow", "getTrace": "[]Span", "listIncomingLinks": "[]Span",
}

func addExample(op map[string]any, doc map[string]any) {
	responses, _ := op["responses"].(map[string]any)
	ok, _ := responses["200"].(map[string]any)
	content, _ := ok["content"].(map[string]any)
	jsonContent, _ := content["application/json"].(map[string]any)
	if jsonContent != nil {
		components := doc["components"].(map[string]any)["schemas"].(map[string]any)
		schema, _ := jsonContent["schema"].(map[string]any)
		jsonContent["examples"] = map[string]any{"example": map[string]any{"summary": "Representative Spaniel response", "value": schemaExample(schema, components, "", 0)}}
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
func schemaExample(schema, components map[string]any, field string, depth int) any {
	if depth > 4 {
		return "…"
	}
	if ref, _ := schema["$ref"].(string); ref != "" {
		const prefix = "#/components/schemas/"
		if name, ok := strings.CutPrefix(ref, prefix); ok {
			if resolved, ok := components[name].(map[string]any); ok {
				return schemaExample(resolved, components, field, depth+1)
			}
		}
	}
	if values, ok := schema["enum"].([]any); ok && len(values) > 0 {
		return values[0]
	}
	for _, key := range []string{"oneOf", "anyOf", "allOf"} {
		if values, ok := schema[key].([]any); ok && len(values) > 0 {
			if option, ok := values[0].(map[string]any); ok {
				return schemaExample(option, components, field, depth+1)
			}
		}
	}
	typ := ""
	switch raw := schema["type"].(type) {
	case string:
		typ = raw
	case []any:
		for _, candidate := range raw {
			if text, ok := candidate.(string); ok && text != "null" {
				typ = text
				break
			}
		}
	}
	switch typ {
	case "array":
		if items, ok := schema["items"].(map[string]any); ok {
			return []any{schemaExample(items, components, field, depth+1)}
		}
		return []any{}
	case "object", "":
		properties, _ := schema["properties"].(map[string]any)
		if len(properties) == 0 {
			return map[string]any{}
		}
		result := map[string]any{}
		for _, name := range examplePropertyNames(properties, schema) {
			if property, ok := properties[name].(map[string]any); ok {
				result[name] = schemaExample(property, components, name, depth+1)
			}
		}
		return result
	case "boolean":
		return true
	case "integer":
		return 1
	case "number":
		return 1.5
	default:
		return exampleString(field)
	}
}

func examplePropertyNames(properties map[string]any, schema map[string]any) []string {
	preferred := []string{"id", "trace_id", "span_id", "session_id", "name", "label", "service_name", "state", "status", "enabled", "timestamp_ns", "start_ns", "duration_ns", "value", "count", "message"}
	seen := map[string]bool{}
	result := make([]string, 0, 8)
	for _, name := range preferred {
		if _, ok := properties[name]; ok {
			result = append(result, name)
			seen[name] = true
		}
	}
	if required, ok := schema["required"].([]any); ok {
		for _, raw := range required {
			if name, ok := raw.(string); ok && !seen[name] {
				result = append(result, name)
				seen[name] = true
			}
		}
	}
	var remaining []string
	for name := range properties {
		if !seen[name] {
			remaining = append(remaining, name)
		}
	}
	sort.Strings(remaining)
	for _, name := range remaining {
		result = append(result, name)
		if len(result) == 8 {
			break
		}
	}
	if len(result) > 8 {
		result = result[:8]
	}
	return result
}

func exampleString(field string) string {
	switch field {
	case "id", "session_id":
		return "session_2026-10-07_1200"
	case "trace_id":
		return "6d8f4a9c2e1b7f30"
	case "span_id":
		return "19ab3f8e"
	case "service_name":
		return "checkout"
	case "name":
		return "POST /checkout"
	case "label":
		return "checkout regression"
	case "message", "body", "detail":
		return "Representative Spaniel response"
	default:
		return "example"
	}
}

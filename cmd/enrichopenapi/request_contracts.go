package main

// applyRequestContracts supplies the parameter and request-body information
// consumed by openapi-fetch. The server still owns validation; this gives the
// browser the matching compile-time contract.
func applyRequestContracts(doc map[string]any) {
	for _, item := range doc["paths"].(map[string]any) {
		for method, raw := range item.(map[string]any) {
			if method == "parameters" {
				continue
			}
			op := raw.(map[string]any)
			id, _ := op["operationId"].(string)
			if params, ok := queryParameters[id]; ok {
				existing, _ := op["parameters"].([]any)
				for _, param := range params {
					existing = append(existing, param)
				}
				op["parameters"] = existing
			}
			body, _ := op["requestBody"].(map[string]any)
			content, _ := body["content"].(map[string]any)
			jsonBody, _ := content["application/json"].(map[string]any)
			if schema, _ := jsonBody["schema"].(map[string]any); schema != nil && schema["$ref"] == "#/components/schemas/JSONValue" {
				jsonBody["schema"] = requestBodySchemas[id]
				if jsonBody["schema"] == nil {
					jsonBody["schema"] = map[string]any{"type": "object", "additionalProperties": true}
				}
			}
		}
	}
}

func query(name, typ string, required bool) map[string]any {
	return map[string]any{"name": name, "in": "query", "required": required, "schema": map[string]any{"type": typ}}
}

var queryParameters = map[string][]any{
	"listAlerts":           {query("page", "integer", false), query("limit", "integer", false), query("state", "string", false), query("search", "string", false)},
	"listAlertEvents":      {query("group_key", "string", false), query("page", "integer", false), query("limit", "integer", false)},
	"listAlertHistory":     {query("page", "integer", false), query("limit", "integer", false), query("rule_id", "string", false), query("state", "string", false), query("kind", "string", false), query("severity", "string", false), query("group_key", "string", false), query("search", "string", false), query("from", "integer", false), query("to", "integer", false)},
	"getCoverage":          {query("sessionId", "string", false)},
	"getDiff":              {query("baseline", "string", true), query("compare", "string", true)},
	"listIssues":           {query("traceId", "string", false), query("sessionId", "string", false)},
	"listLint":             {query("sessionId", "string", false)},
	"listLogs":             {query("sessionId", "string", false), query("traceId", "string", false), query("spanId", "string", false), query("severity", "string", false), query("service", "string", false), query("page", "integer", false), query("limit", "integer", false)},
	"listMetrics":          {query("sessionId", "string", false)},
	"getMetricCardinality": {query("sessionId", "string", false)},
	"getMetricSeries":      {query("name", "string", true), query("service", "string", false), query("sessionId", "string", false), query("from", "integer", false), query("to", "integer", false), query("operation", "string", false), query("with_traces", "boolean", false)},
	"listQueryCatalog":     {query("signal", "string", false), query("q", "string", false)},
	"searchTelemetry":      {query("q", "string", true), query("sessionId", "string", false), query("limit", "integer", false)},
	"getServiceMap":        {query("sessionId", "string", false)},
	"importSession":        {query("label", "string", true), query("format", "string", true)},
	"listSources":          {query("sessionId", "string", false)},
	"listSpans":            {query("view", "string", false), query("sort", "string", false), query("sessionId", "string", false), query("limit", "integer", false), query("page", "integer", false), query("service", "string", false), query("name", "string", false), query("kind", "integer", false)},
	"getStats":             {query("sessionId", "string", false)},
	"listTraces":           {query("sessionId", "string", false), query("service", "string", false), query("page", "integer", false), query("limit", "integer", false)},
}

var requestBodySchemas = map[string]any{
	"createSession":              map[string]any{"type": "object", "properties": map[string]any{"label": map[string]any{"type": "string"}}},
	"patchSession":               map[string]any{"type": "object", "properties": map[string]any{"label": map[string]any{"type": "string"}, "note": map[string]any{"type": "string"}}},
	"setSessionBaseline":         map[string]any{"type": "object", "required": []any{"is_baseline"}, "properties": map[string]any{"is_baseline": map[string]any{"type": "boolean"}}},
	"moveDashboardPanel":         map[string]any{"type": "object", "required": []any{"direction"}, "properties": map[string]any{"direction": map[string]any{"type": "integer", "enum": []any{-1, 1}}}},
	"reorderDashboards":          map[string]any{"type": "object", "required": []any{"ids"}, "properties": map[string]any{"ids": map[string]any{"type": "array", "items": map[string]any{"type": "string"}}}},
	"testAlertNotification":      map[string]any{"type": "object", "required": []any{"destination"}, "properties": map[string]any{"destination": map[string]any{"type": "string", "enum": []any{"browser", "pushover"}}}},
	"acknowledgeAlertInstance":   map[string]any{"type": "object", "required": []any{"group_key"}, "properties": map[string]any{"group_key": map[string]any{"type": "string"}, "note": map[string]any{"type": "string"}}},
	"unacknowledgeAlertInstance": map[string]any{"type": "object", "required": []any{"group_key"}, "properties": map[string]any{"group_key": map[string]any{"type": "string"}}},
	"createAlertSilence":         map[string]any{"type": "object", "required": []any{"ends_at", "comment"}, "properties": map[string]any{"ends_at": map[string]any{"type": "integer"}, "comment": map[string]any{"type": "string"}, "group_key": map[string]any{"type": "string"}, "starts_at": map[string]any{"type": "integer"}}},
	"patchAlertSilence":          map[string]any{"type": "object", "required": []any{"ends_at", "comment", "starts_at"}, "properties": map[string]any{"ends_at": map[string]any{"type": "integer"}, "comment": map[string]any{"type": "string"}, "group_key": map[string]any{"type": "string"}, "starts_at": map[string]any{"type": "integer"}}},
}

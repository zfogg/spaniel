package main

func applyNotificationContracts(doc map[string]any) {
	paths := doc["paths"].(map[string]any)
	paths["/api/notifications"] = map[string]any{
		"get": operation("listNotifications", "list Notifications", "[]NotificationRecord", []any{query("page", "integer", false), query("limit", "integer", false), query("source", "string", false)}),
	}
	paths["/api/notifications/{id}/read"] = map[string]any{
		"post": operation("readNotification", "read Notification", "Ok", []any{pathParameter("id")}),
	}
	paths["/api/notifications/{id}/acknowledge"] = map[string]any{
		"post": operation("acknowledgeNotification", "acknowledge Notification", "Ok", []any{pathParameter("id")}),
	}
}

func pathParameter(name string) map[string]any {
	return map[string]any{"name": name, "in": "path", "required": true, "schema": map[string]any{"type": "string"}}
}

func operation(id, summary, response string, parameters []any) map[string]any {
	data := map[string]any{"$ref": "#/components/schemas/" + response}
	if len(response) > 2 && response[:2] == "[]" {
		data = map[string]any{"type": "array", "items": map[string]any{"$ref": "#/components/schemas/" + response[2:]}}
	}
	op := map[string]any{
		"operationId": id,
		"summary":     summary,
		"tags":        []any{"Administration"},
		"responses": map[string]any{
			"200": map[string]any{
				"description": "Successful response",
				"content": map[string]any{
					"application/json": map[string]any{
						"schema": map[string]any{
							"type":     "object",
							"required": []any{"data", "meta"},
							"properties": map[string]any{
								"data": data,
								"meta": map[string]any{"$ref": "#/components/schemas/Meta"},
							},
						},
					},
				},
			},
			"400": map[string]any{"$ref": "#/components/responses/BadRequest"},
			"404": map[string]any{"$ref": "#/components/responses/NotFound"},
		},
	}
	if parameters != nil {
		op["parameters"] = parameters
	}
	return op
}

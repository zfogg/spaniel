package mcp

import (
	"context"
	"strconv"
	"strings"
	"testing"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/zfogg/spaniel/internal/model"
	"github.com/zfogg/spaniel/internal/storage"
)

func callText(t *testing.T, cs *mcpsdk.ClientSession, name string, args map[string]any) string {
	t.Helper()
	result, err := cs.CallTool(context.Background(), &mcpsdk.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("call %s: %v", name, err)
	}
	if result.IsError {
		t.Fatalf("%s returned tool error: %+v", name, result.Content)
	}
	if result.StructuredContent != nil || len(result.Content) != 1 {
		t.Fatalf("%s returned duplicated or unexpected content: %#v", name, result)
	}
	text, ok := result.Content[0].(*mcpsdk.TextContent)
	if !ok {
		t.Fatalf("%s returned %T, want TextContent", name, result.Content[0])
	}
	return text.Text
}

func TestDashboardYAMLToolsCreateAndUpdateStableID(t *testing.T) {
	cs, _ := connect(t, Options{Version: "t", AllowWrites: true})
	created := callStructured[ApplyDashboardOutput](t, cs, "import_dashboard_yaml", map[string]any{"yaml": "version: 1\nid: dashboard-mcp-test\nname: Before\npanels:\n  - title: Count\n    display_type: single_value\n    query: SELECT 1 AS value\n    position: 0\n"})
	if !created.Applied || created.Dashboard.ID != "dashboard-mcp-test" {
		t.Fatalf("create result = %#v", created)
	}
	panel := created.Dashboard.Panels[0]
	run := callStructured[DashboardPanelResult](t, cs, "run_dashboard_panel", map[string]any{"dashboard_id": created.Dashboard.ID, "panel_id": panel.ID})
	if run.RowCount != 1 || len(run.Columns) != 1 || run.Columns[0] != "value" {
		t.Fatalf("panel result = %#v", run)
	}
	definition := map[string]any{"name": "Applied", "panels": []any{map[string]any{"title": "Count", "display_type": "single_value", "query": "SELECT 2 AS value", "position": 0}}}
	validated := callStructured[DashboardValidationOutput](t, cs, "validate_dashboard", map[string]any{"definition": definition})
	if !validated.Valid || len(validated.Panels) != 1 {
		t.Fatalf("dashboard validation = %#v", validated)
	}
	applied := callStructured[ApplyDashboardOutput](t, cs, "apply_dashboard", map[string]any{"dashboard_id": created.Dashboard.ID, "expected_updated_at": strconv.FormatInt(created.Dashboard.UpdatedAt, 10), "definition": definition})
	if !applied.Applied || applied.Dashboard.Name != "Applied" {
		t.Fatalf("dashboard apply = %#v", applied)
	}
	exported := callText(t, cs, "export_dashboard_yaml", map[string]any{"dashboard_id": created.Dashboard.ID})
	if !strings.Contains(exported, "id: dashboard-mcp-test") {
		t.Fatalf("dashboard export omitted id:\n%s", exported)
	}
	edited := strings.Replace(exported, "name: Applied", "name: After", 1)
	updated := callStructured[ApplyDashboardOutput](t, cs, "import_dashboard_yaml", map[string]any{"yaml": edited})
	if updated.Dashboard.ID != created.Dashboard.ID || updated.Dashboard.Name != "After" {
		t.Fatalf("update result = %#v", updated)
	}
	listed := callStructured[ListDashboardsOutput](t, cs, "list_dashboards", nil)
	if listed.Count != 1 {
		t.Fatalf("dashboard count = %d, want stable-id update", listed.Count)
	}
}

func TestAlertYAMLToolsAndCoverageSpecs(t *testing.T) {
	cs, _ := connect(t, Options{Version: "t", AllowWrites: true})
	alertYAML := "version: 1\nid: alert-mcp-test\nname: Error count\nquery: SELECT count(*) AS value FROM telemetry_spans\ncondition:\n  kind: threshold\n  operator: '>'\n  threshold: 0\npending_for: 0s\ncooldown: 0s\nseverity: warning\nenabled: true\nbrowser_enabled: true\npushover_enabled: false\n"
	alert := callStructured[ApplyAlertOutput](t, cs, "import_alert_yaml", map[string]any{"yaml": alertYAML})
	if !alert.Applied || alert.Rule.ID != "alert-mcp-test" {
		t.Fatalf("alert import = %#v", alert)
	}
	listedAlerts := callStructured[ListAlertsOutput](t, cs, "list_alerts", map[string]any{"state": "all"})
	if listedAlerts.Total != 1 {
		t.Fatalf("alerts = %#v", listedAlerts)
	}
	detail := callStructured[AlertOutput](t, cs, "get_alert", map[string]any{"alert_id": alert.Rule.ID})
	if detail.Rule.ID != alert.Rule.ID || detail.State != "resolved" {
		t.Fatalf("alert detail = %#v", detail)
	}
	preview := callStructured[AlertPreviewOutput](t, cs, "preview_alert", map[string]any{"alert_id": alert.Rule.ID})
	if preview.RowCount != 1 || len(preview.Columns) != 1 || preview.Columns[0] != "value" {
		t.Fatalf("alert preview = %#v", preview)
	}
	definition := map[string]any{"name": "Updated error count", "query": "SELECT count(*) AS value FROM telemetry_spans", "condition": map[string]any{"kind": "threshold", "operator": ">", "threshold": 0}, "pending_for": "0s", "cooldown": "0s", "severity": "critical", "enabled": true, "browser_enabled": true, "pushover_enabled": false}
	validated := callStructured[AlertValidationOutput](t, cs, "validate_alert", map[string]any{"definition": definition})
	if !validated.Valid || len(validated.Columns) != 1 {
		t.Fatalf("alert validation = %#v", validated)
	}
	applied := callStructured[ApplyAlertOutput](t, cs, "apply_alert", map[string]any{"alert_id": alert.Rule.ID, "expected_updated_at": strconv.FormatInt(alert.Rule.UpdatedAt, 10), "definition": definition})
	if !applied.Applied || applied.Rule.Name != "Updated error count" {
		t.Fatalf("alert apply = %#v", applied)
	}
	exported := callText(t, cs, "export_alert_yaml", map[string]any{"alert_id": alert.Rule.ID})
	if !strings.Contains(exported, "id: alert-mcp-test") {
		t.Fatalf("alert export omitted id:\n%s", exported)
	}

	openapi := "openapi: 3.1.0\ninfo: {title: Test, version: '1'}\npaths:\n  /orders:\n    get:\n      responses: {'200': {description: ok}}\n"
	spec := callStructured[ApplyCoverageSpecOutput](t, cs, "apply_coverage_spec", map[string]any{"name": "Orders", "service_name": "api", "openapi": openapi})
	if !spec.Applied || spec.Spec.RouteCount != 1 {
		t.Fatalf("coverage spec = %#v", spec)
	}
	listed := callStructured[ListCoverageSpecsOutput](t, cs, "list_coverage_specs", nil)
	if listed.Count != 1 || listed.Specs[0].Content != "" {
		t.Fatalf("coverage list = %#v", listed)
	}
	gotSpec := callStructured[model.CoverageSpec](t, cs, "get_coverage_spec", map[string]any{"spec_id": spec.Spec.ID})
	if gotSpec.Content != openapi {
		t.Fatalf("coverage document was not returned")
	}
	report := callStructured[CoverageOutput](t, cs, "get_coverage", nil)
	if report.SessionID == "" {
		t.Fatalf("coverage report omitted session: %#v", report)
	}
}

func TestNotificationInboxAndIngestionHealth(t *testing.T) {
	cs, store := connect(t, Options{Version: "t", AllowWrites: true})
	notification := &model.NotificationRecord{ID: "notice-1", Source: "alert", SourceID: "rule-1", Severity: "warning", Title: "Needs attention", Body: "test"}
	if err := store.RecordNotification(notification); err != nil {
		t.Fatal(err)
	}
	inbox := callStructured[ListNotificationsOutput](t, cs, "list_notifications", nil)
	if inbox.Total != 1 || len(inbox.Items) != 1 {
		t.Fatalf("inbox = %#v", inbox)
	}
	callStructured[MutationOutput](t, cs, "acknowledge_notification", map[string]any{"notification_id": notification.ID})
	inbox = callStructured[ListNotificationsOutput](t, cs, "list_notifications", nil)
	if inbox.Items[0].ReadAt == nil || inbox.Items[0].AcknowledgedAt == nil {
		t.Fatalf("notification was not acknowledged: %#v", inbox.Items[0])
	}
	health := callStructured[IngestionHealthOutput](t, cs, "get_ingestion_health", nil)
	if !health.Healthy || health.StorageFull {
		t.Fatalf("new store should be healthy: %#v", health)
	}
}

func TestDynamicResourceTemplatesAndDashboardResource(t *testing.T) {
	cs, store := connect(t, Options{Version: "t"})
	sessionID := store.ActiveSessionID()
	seedTrace(t, store, sessionID)
	dashboard, err := store.CreateDashboardWithPanels("Resource dashboard", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	result, err := cs.ListResourceTemplates(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{
		"spaniel://dashboards/{dashboard_id}":             false,
		"spaniel://alerts/{alert_id}":                     false,
		"spaniel://spans/{span_id}":                       false,
		"spaniel://traces/{trace_id}":                     false,
		"spaniel://logs/{session_id}/{timestamp_ns}":      false,
		"spaniel://metrics/{session_id}/{service}/{name}": false,
	}
	for _, template := range result.ResourceTemplates {
		if _, ok := want[template.URITemplate]; ok {
			want[template.URITemplate] = true
		}
	}
	for uri, found := range want {
		if !found {
			t.Errorf("resource template %q not registered", uri)
		}
	}
	if err := store.CreateAlertRule(&storage.AlertRule{ID: "resource-alert", Name: "Resource alert", QuerySQL: "SELECT 1 AS value", ConditionJSON: `{"kind":"threshold","operator":">","value":0}`, GroupByJSON: "[]", Severity: "warning", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if err := store.InsertMetric(&storage.Metric{Name: "requests", Type: "counter", TimestampNs: 3_000_000, Value: 7, Attributes: "{}", ServiceName: "api", SessionID: sessionID}); err != nil {
		t.Fatal(err)
	}
	resources := map[string]string{
		"spaniel://dashboards/" + dashboard.ID:             "Resource dashboard",
		"spaniel://alerts/resource-alert":                  "Resource alert",
		"spaniel://spans/" + dbSpanID:                      "SELECT users",
		"spaniel://traces/" + testTraceID:                  "GET /api/users",
		"spaniel://logs/" + sessionID + "/2600000":         "query failed",
		"spaniel://metrics/" + sessionID + "/api/requests": "requests",
	}
	for uri, expected := range resources {
		resource, err := cs.ReadResource(context.Background(), &mcpsdk.ReadResourceParams{URI: uri})
		if err != nil {
			t.Errorf("read %s: %v", uri, err)
			continue
		}
		if len(resource.Contents) != 1 || resource.Contents[0].MIMEType != "text/toon" || !strings.Contains(resource.Contents[0].Text, expected) {
			t.Errorf("resource %s = %#v, want TOON containing %q", uri, resource.Contents, expected)
		}
	}
}

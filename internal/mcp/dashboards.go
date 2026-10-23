package mcp

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/google/uuid"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"gopkg.in/yaml.v3"

	"github.com/zfogg/spaniel/internal/dashboardconfig"
	"github.com/zfogg/spaniel/internal/model"
	"github.com/zfogg/spaniel/internal/storage"
)

func (h *handler) registerDashboardTools(s *mcpsdk.Server) {
	readOnly := &mcpsdk.ToolAnnotations{ReadOnlyHint: true}
	addTOONTool(s, &mcpsdk.Tool{Name: "list_dashboards", Description: "List saved dashboards with their panels and variables. Optionally search names and descriptions.", Annotations: readOnly}, h.listDashboards)
	addTOONTool(s, &mcpsdk.Tool{Name: "get_dashboard", Description: "Get one complete dashboard definition, including panels, variables, SQL, settings, and layout.", Annotations: readOnly}, h.getDashboard)
	addTOONTool(s, &mcpsdk.Tool{Name: "run_dashboard_panel", Description: "Run one saved dashboard panel with validated variable overrides and return its TOON-ready tabular result.", Annotations: readOnly}, h.runDashboardPanel)
	addTOONTool(s, &mcpsdk.Tool{Name: "validate_dashboard", Description: "Validate a structured dashboard definition and execute each panel query without saving it.", Annotations: readOnly}, h.validateDashboard)
	addTextTool(s, &mcpsdk.Tool{Name: "export_dashboard_yaml", Description: "Export one dashboard in Spaniel's portable YAML format.", Annotations: readOnly}, h.exportDashboardYAML)

	if !h.opts.AllowWrites {
		return
	}
	addTOONTool(s, &mcpsdk.Tool{Name: "apply_dashboard", Description: "Create or replace a complete structured dashboard definition. Supports dry-run and optimistic updated_at checks.", Annotations: &mcpsdk.ToolAnnotations{DestructiveHint: boolPtr(false), IdempotentHint: true}}, h.applyDashboard)
	addTOONTool(s, &mcpsdk.Tool{Name: "import_dashboard_yaml", Description: "Validate and import portable dashboard YAML. A stable id updates or creates that dashboard; omit id to create a new dashboard. Supports dry-run.", Annotations: &mcpsdk.ToolAnnotations{DestructiveHint: boolPtr(false)}}, h.importDashboardYAML)
	addTOONTool(s, &mcpsdk.Tool{Name: "delete_dashboard", Description: "Delete a local dashboard and its panels and variables. File-managed dashboards cannot be deleted.", Annotations: &mcpsdk.ToolAnnotations{DestructiveHint: boolPtr(true)}}, h.deleteDashboard)
}

type ListDashboardsInput struct {
	Search        string `json:"search,omitempty" jsonschema:"case-insensitive name or description substring"`
	ManagedByFile *bool  `json:"managed_by_file,omitempty" jsonschema:"only file-managed (true) or local (false) dashboards"`
	Limit         int    `json:"limit,omitempty" jsonschema:"max dashboards to return (default 100, max 500)"`
	Page          int    `json:"page,omitempty" jsonschema:"1-based page number (default 1)"`
}

type DashboardSummary struct {
	Updated       bool   `json:"updated"`
	ID            string `json:"id"`
	Name          string `json:"name"`
	Description   string `json:"description,omitempty"`
	PanelCount    int    `json:"panel_count"`
	VariableCount int    `json:"variable_count"`
	ManagedByFile bool   `json:"managed_by_file"`
	UpdatedAt     int64  `json:"updated_at"`
	Link          string `json:"link"`
}

type ListDashboardsOutput struct {
	Count      int                `json:"count"`
	Dashboards []DashboardSummary `json:"dashboards"`
}

func (h *handler) listDashboards(ctx context.Context, _ *mcpsdk.CallToolRequest, in ListDashboardsInput) (*mcpsdk.CallToolResult, ListDashboardsOutput, error) {
	dashboards, err := h.store.WithContext(ctx).ListDashboards()
	if err != nil {
		return nil, ListDashboardsOutput{}, fmt.Errorf("list dashboards: %w", err)
	}
	needle := strings.ToLower(strings.TrimSpace(in.Search))
	limit, page := in.Limit, in.Page
	if limit <= 0 {
		limit = 100
	}
	if limit > 500 {
		limit = 500
	}
	if page < 1 {
		page = 1
	}
	out := ListDashboardsOutput{Dashboards: []DashboardSummary{}}
	matched, start := 0, (page-1)*limit
	for _, dashboard := range dashboards {
		managed := dashboard.SourceFile != ""
		if needle != "" && !strings.Contains(strings.ToLower(dashboard.Name+"\n"+dashboard.Description), needle) || in.ManagedByFile != nil && managed != *in.ManagedByFile {
			continue
		}
		if matched < start {
			matched++
			continue
		}
		matched++
		out.Dashboards = append(out.Dashboards, DashboardSummary{Updated: dashboard.Updated, ID: dashboard.ID, Name: dashboard.Name, Description: dashboard.Description, PanelCount: len(dashboard.Panels), VariableCount: len(dashboard.Variables), ManagedByFile: managed, UpdatedAt: dashboard.UpdatedAt, Link: "/dashboards?id=" + dashboard.ID})
		if len(out.Dashboards) >= limit {
			break
		}
	}
	out.Count = len(out.Dashboards)
	return nil, out, nil
}

type GetDashboardInput struct {
	DashboardID string `json:"dashboard_id" jsonschema:"dashboard id from list_dashboards"`
}

type DashboardOutput struct {
	Dashboard *model.Dashboard `json:"dashboard"`
	Link      string           `json:"link"`
}

func (h *handler) getDashboard(ctx context.Context, _ *mcpsdk.CallToolRequest, in GetDashboardInput) (*mcpsdk.CallToolResult, DashboardOutput, error) {
	dashboard, err := h.store.WithContext(ctx).GetDashboard(in.DashboardID)
	if err != nil {
		return nil, DashboardOutput{}, fmt.Errorf("dashboard not found")
	}
	if err := h.store.WithContext(ctx).MarkDefinitionSeen("dashboard", dashboard.ID, dashboard.SourceHash, dashboard.UpdatedAt); err != nil {
		return nil, DashboardOutput{}, err
	}
	return nil, DashboardOutput{Dashboard: dashboard, Link: "/dashboards?id=" + dashboard.ID}, nil
}

type RunDashboardPanelInput struct {
	DashboardID string            `json:"dashboard_id"`
	PanelID     string            `json:"panel_id"`
	Variables   map[string]string `json:"variables,omitempty" jsonschema:"dashboard variable overrides by name"`
	MaxRows     int               `json:"max_rows,omitempty" jsonschema:"maximum returned rows; default 1000, max 5000"`
}

type DashboardPanelResult struct {
	DashboardID string            `json:"dashboard_id"`
	PanelID     string            `json:"panel_id"`
	Title       string            `json:"title"`
	DisplayType string            `json:"display_type"`
	Variables   map[string]string `json:"variables"`
	Columns     []string          `json:"columns"`
	Rows        [][]any           `json:"rows"`
	RowCount    int               `json:"row_count"`
	Truncated   bool              `json:"truncated"`
}

func (h *handler) runDashboardPanel(ctx context.Context, _ *mcpsdk.CallToolRequest, in RunDashboardPanelInput) (*mcpsdk.CallToolResult, DashboardPanelResult, error) {
	dashboard, err := h.store.WithContext(ctx).GetDashboard(in.DashboardID)
	if err != nil {
		return nil, DashboardPanelResult{}, fmt.Errorf("dashboard not found")
	}
	var panel *model.DashboardPanel
	for _, candidate := range dashboard.Panels {
		if candidate.ID == in.PanelID {
			panel = candidate
			break
		}
	}
	if panel == nil {
		return nil, DashboardPanelResult{}, fmt.Errorf("dashboard panel not found")
	}
	values, args, err := h.dashboardArgs(dashboard, panel.QuerySQL, in.Variables)
	if err != nil {
		return nil, DashboardPanelResult{}, err
	}
	maxRows := in.MaxRows
	if maxRows <= 0 {
		maxRows = 1000
	}
	if maxRows > 5000 {
		maxRows = 5000
	}
	columns, rows, truncated, err := h.store.WithContext(ctx).ReadOnlyQueryArgs(ctx, panel.QuerySQL, args, maxRows)
	if err != nil {
		return nil, DashboardPanelResult{}, fmt.Errorf("run panel query: %w", err)
	}
	return nil, DashboardPanelResult{DashboardID: dashboard.ID, PanelID: panel.ID, Title: panel.Title, DisplayType: panel.DisplayType, Variables: values, Columns: columns, Rows: rows, RowCount: len(rows), Truncated: truncated}, nil
}

type DashboardVariableDefinition struct {
	Name         string   `json:"name"`
	Kind         string   `json:"kind"`
	Source       string   `json:"source"`
	Options      []string `json:"options,omitempty"`
	DefaultValue string   `json:"default_value,omitempty"`
}

type DashboardPanelDefinition struct {
	Title       string         `json:"title"`
	DisplayType string         `json:"display_type"`
	Query       string         `json:"query"`
	Settings    map[string]any `json:"settings,omitempty"`
	Layout      map[string]any `json:"layout,omitempty"`
	Position    int            `json:"position"`
}

type DashboardDefinition struct {
	Name        string                        `json:"name"`
	Description string                        `json:"description,omitempty"`
	Variables   []DashboardVariableDefinition `json:"variables,omitempty"`
	Panels      []DashboardPanelDefinition    `json:"panels,omitempty"`
}

type ValidateDashboardInput struct {
	Definition DashboardDefinition `json:"definition"`
}

type DashboardValidationOutput struct {
	Valid  bool             `json:"valid"`
	Panels []map[string]any `json:"panels"`
}

func (h *handler) validateDashboard(ctx context.Context, _ *mcpsdk.CallToolRequest, in ValidateDashboardInput) (*mcpsdk.CallToolResult, DashboardValidationOutput, error) {
	_, dashboard, err := dashboardFromInput(in.Definition, "validation")
	if err != nil {
		return nil, DashboardValidationOutput{}, err
	}
	out, err := h.validateDashboardModel(ctx, dashboard)
	return nil, out, err
}

func (h *handler) validateDashboardModel(ctx context.Context, dashboard *model.Dashboard) (DashboardValidationOutput, error) {
	out := DashboardValidationOutput{Valid: true, Panels: []map[string]any{}}
	for _, panel := range dashboard.Panels {
		if err := storage.ValidateReadOnlySQL(panel.QuerySQL); err != nil {
			return DashboardValidationOutput{}, fmt.Errorf("panel %q: %w", panel.Title, err)
		}
		_, args, err := h.dashboardArgs(dashboard, panel.QuerySQL, nil)
		if err != nil {
			return DashboardValidationOutput{}, err
		}
		columns, _, _, err := h.store.WithContext(ctx).ReadOnlyQueryArgs(ctx, panel.QuerySQL, args, 1)
		if err != nil {
			return DashboardValidationOutput{}, fmt.Errorf("panel %q: %w", panel.Title, err)
		}
		if err := dashboardconfig.ValidatePanelResult(panel.DisplayType, columns); err != nil {
			return DashboardValidationOutput{}, fmt.Errorf("panel %q: %w", panel.Title, err)
		}
		if panel.DisplayType == "deploy_correlation" {
			var settings map[string]any
			if err := json.Unmarshal([]byte(panel.SettingsJSON), &settings); err != nil {
				return DashboardValidationOutput{}, fmt.Errorf("panel %q settings: %w", panel.Title, err)
			}
			if annotationSQL, _ := settings["annotation_query"].(string); strings.TrimSpace(annotationSQL) != "" {
				if err := storage.ValidateReadOnlySQL(annotationSQL); err != nil {
					return DashboardValidationOutput{}, fmt.Errorf("panel %q annotation_query: %w", panel.Title, err)
				}
				_, annotationArgs, err := h.dashboardArgs(dashboard, annotationSQL, nil)
				if err != nil {
					return DashboardValidationOutput{}, err
				}
				annotationColumns, _, _, err := h.store.WithContext(ctx).ReadOnlyQueryArgs(ctx, annotationSQL, annotationArgs, 1)
				if err != nil {
					return DashboardValidationOutput{}, fmt.Errorf("panel %q annotation_query: %w", panel.Title, err)
				}
				if err := dashboardconfig.ValidateDeployAnnotationResult(annotationColumns); err != nil {
					return DashboardValidationOutput{}, fmt.Errorf("panel %q: %w", panel.Title, err)
				}
			}
		}
		out.Panels = append(out.Panels, map[string]any{"title": panel.Title, "display_type": panel.DisplayType, "columns": columns})
	}
	return out, nil
}

type ApplyDashboardInput struct {
	DashboardID       string              `json:"dashboard_id,omitempty" jsonschema:"existing dashboard id to replace; omit to create"`
	Definition        DashboardDefinition `json:"definition"`
	ExpectedUpdatedAt string              `json:"expected_updated_at,omitempty" jsonschema:"exact decimal updated_at returned by get_dashboard; rejects a stale replacement"`
	DryRun            bool                `json:"dry_run,omitempty"`
}

type ApplyDashboardOutput struct {
	Applied   bool             `json:"applied"`
	Dashboard *model.Dashboard `json:"dashboard"`
	Link      string           `json:"link"`
}

func (h *handler) applyDashboard(ctx context.Context, _ *mcpsdk.CallToolRequest, in ApplyDashboardInput) (*mcpsdk.CallToolResult, ApplyDashboardOutput, error) {
	if _, _, err := h.validateDashboard(ctx, nil, ValidateDashboardInput{Definition: in.Definition}); err != nil {
		return nil, ApplyDashboardOutput{}, err
	}
	id := in.DashboardID
	if id == "" {
		id = uuid.NewString()
	} else {
		existing, err := h.store.WithContext(ctx).GetDashboard(id)
		if err != nil {
			return nil, ApplyDashboardOutput{}, fmt.Errorf("dashboard not found")
		}
		if existing.SourceFile != "" {
			return nil, ApplyDashboardOutput{}, fmt.Errorf("dashboard is managed by YAML file %s", existing.SourceFile)
		}
		expected, err := parseExpectedUpdatedAt(in.ExpectedUpdatedAt)
		if err != nil {
			return nil, ApplyDashboardOutput{}, err
		}
		if expected != 0 && existing.UpdatedAt != expected {
			return nil, ApplyDashboardOutput{}, fmt.Errorf("dashboard changed since it was read: expected updated_at %d, got %d", expected, existing.UpdatedAt)
		}
	}
	_, dashboard, err := dashboardFromInput(in.Definition, id)
	if err != nil {
		return nil, ApplyDashboardOutput{}, err
	}
	if in.DryRun {
		return nil, ApplyDashboardOutput{Applied: false, Dashboard: dashboard, Link: "/dashboards?id=" + id}, nil
	}
	if err := h.store.WithContext(ctx).ReplaceDashboardDefinition(dashboard); err != nil {
		return nil, ApplyDashboardOutput{}, fmt.Errorf("apply dashboard: %w", err)
	}
	saved, err := h.store.WithContext(ctx).GetDashboard(id)
	if err != nil {
		return nil, ApplyDashboardOutput{}, fmt.Errorf("read applied dashboard: %w", err)
	}
	return nil, ApplyDashboardOutput{Applied: true, Dashboard: saved, Link: "/dashboards?id=" + id}, nil
}

func parseExpectedUpdatedAt(value string) (int64, error) {
	if value == "" {
		return 0, nil
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil || parsed < 0 {
		return 0, fmt.Errorf("expected_updated_at must be a non-negative decimal timestamp string")
	}
	return parsed, nil
}

type ImportDashboardYAMLInput struct {
	YAML   string `json:"yaml"`
	DryRun bool   `json:"dry_run,omitempty"`
}

func (h *handler) importDashboardYAML(ctx context.Context, _ *mcpsdk.CallToolRequest, in ImportDashboardYAMLInput) (*mcpsdk.CallToolResult, ApplyDashboardOutput, error) {
	definition, err := dashboardconfig.Parse([]byte(in.YAML))
	if err != nil {
		return nil, ApplyDashboardOutput{}, fmt.Errorf("invalid dashboard YAML: %w", err)
	}
	id := uuid.NewString()
	dashboard, err := definition.Dashboard(id)
	if err != nil {
		return nil, ApplyDashboardOutput{}, err
	}
	if existing, err := h.store.WithContext(ctx).GetDashboard(dashboard.ID); err == nil && existing.SourceFile != "" {
		return nil, ApplyDashboardOutput{}, fmt.Errorf("dashboard is managed by YAML file %s; remove id to import a local copy", existing.SourceFile)
	}
	id = dashboard.ID
	if _, err := h.validateDashboardModel(ctx, dashboard); err != nil {
		return nil, ApplyDashboardOutput{}, err
	}
	if in.DryRun {
		return nil, ApplyDashboardOutput{Applied: false, Dashboard: dashboard, Link: "/dashboards?id=" + id}, nil
	}
	if err := h.store.WithContext(ctx).ReplaceDashboardDefinition(dashboard); err != nil {
		return nil, ApplyDashboardOutput{}, fmt.Errorf("import dashboard: %w", err)
	}
	saved, err := h.store.WithContext(ctx).GetDashboard(id)
	if err != nil {
		return nil, ApplyDashboardOutput{}, err
	}
	return nil, ApplyDashboardOutput{Applied: true, Dashboard: saved, Link: "/dashboards?id=" + id}, nil
}

func (h *handler) exportDashboardYAML(ctx context.Context, _ *mcpsdk.CallToolRequest, in GetDashboardInput) (string, error) {
	dashboard, err := h.store.WithContext(ctx).GetDashboard(in.DashboardID)
	if err != nil {
		return "", fmt.Errorf("dashboard not found")
	}
	data, err := dashboardconfig.Marshal(dashboard)
	return string(data), err
}

type DeleteDashboardInput struct {
	DashboardID string `json:"dashboard_id"`
}

type MutationOutput struct {
	OK bool `json:"ok"`
}

func (h *handler) deleteDashboard(ctx context.Context, _ *mcpsdk.CallToolRequest, in DeleteDashboardInput) (*mcpsdk.CallToolResult, MutationOutput, error) {
	dashboard, err := h.store.WithContext(ctx).GetDashboard(in.DashboardID)
	if err != nil {
		return nil, MutationOutput{}, fmt.Errorf("dashboard not found")
	}
	if dashboard.SourceFile != "" {
		return nil, MutationOutput{}, fmt.Errorf("dashboard is managed by YAML file %s", dashboard.SourceFile)
	}
	if err := h.store.WithContext(ctx).DeleteDashboard(in.DashboardID); err != nil {
		return nil, MutationOutput{}, err
	}
	return nil, MutationOutput{OK: true}, nil
}

func dashboardFromInput(in DashboardDefinition, id string) (dashboardconfig.Definition, *model.Dashboard, error) {
	definition := dashboardconfig.Definition{Version: dashboardconfig.Version, Name: in.Name, Description: in.Description, MagicVariables: dashboardconfig.MagicVariables}
	for _, variable := range in.Variables {
		definition.Variables = append(definition.Variables, dashboardconfig.Variable{Name: variable.Name, Kind: variable.Kind, Source: variable.Source, Options: variable.Options, DefaultValue: variable.DefaultValue})
	}
	for _, panel := range in.Panels {
		definition.Panels = append(definition.Panels, dashboardconfig.Panel{Title: panel.Title, DisplayType: panel.DisplayType, Query: panel.Query, Settings: panel.Settings, Layout: panel.Layout, Position: panel.Position})
	}
	// Parse through the portable contract so structured MCP and YAML imports
	// share the same validation rules.
	data, err := yaml.Marshal(definition)
	if err != nil {
		return definition, nil, err
	}
	definition, err = dashboardconfig.Parse(data)
	if err != nil {
		return definition, nil, fmt.Errorf("invalid dashboard: %w", err)
	}
	dashboard, err := definition.Dashboard(id)
	return definition, dashboard, err
}

func (h *handler) dashboardArgs(dashboard *model.Dashboard, query string, overrides map[string]string) (map[string]string, []any, error) {
	values := map[string]string{}
	known := map[string]bool{}
	for _, variable := range dashboard.Variables {
		values[variable.Name] = variable.DefaultValue
		known[variable.Name] = true
	}
	for name, value := range overrides {
		if !known[name] {
			return nil, nil, fmt.Errorf("unknown dashboard variable %q", name)
		}
		values[name] = value
	}
	values["session_id"] = h.store.ActiveSessionID()
	args := make([]any, 0, len(values))
	for name, value := range values {
		if regexp.MustCompile(`\$` + regexp.QuoteMeta(name) + `\b`).MatchString(query) {
			args = append(args, sql.Named(name, value))
		}
	}
	return values, args, nil
}

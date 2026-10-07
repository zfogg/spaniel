package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/zfogg/spaniel/internal/dashboardconfig"
	"github.com/zfogg/spaniel/internal/storage"
)

const dashboardQueryVersion = 1

type dashboardInput struct {
	Name        string `json:"name" validate:"required,max=120"`
	Description string `json:"description" validate:"max=1000"`
}
type panelInput struct {
	Title        string `json:"title" validate:"required,max=160"`
	DisplayType  string `json:"display_type" validate:"required,oneof=single_value time_series table heatmap entity_list trace_list span_list log_list deploy_correlation"`
	QuerySQL     string `json:"query_sql" validate:"required,max=16000"`
	SettingsJSON string `json:"settings_json"`
	LayoutJSON   string `json:"layout_json"`
	Position     int    `json:"position"`
}
type variableInput struct {
	Name         string `json:"name" validate:"required,max=64"`
	Kind         string `json:"kind" validate:"required,oneof=attribute string number boolean duration time enum service operation trace_id span_id log_id"`
	Source       string `json:"source" validate:"required,max=160"`
	OptionsJSON  string `json:"options_json"`
	DefaultValue string `json:"default_value"`
}

func (r *Router) listDashboards(w http.ResponseWriter, req *http.Request) {
	xs, err := r.store.WithContext(req.Context()).ListDashboards()
	if err != nil {
		respondErr(w, req, 500, err.Error())
		return
	}
	respond(w, xs, len(xs), 1)
}
func (r *Router) createDashboard(w http.ResponseWriter, req *http.Request) {
	var in dashboardInput
	if !decodeAndValidate(w, req, &in) {
		return
	}
	x, err := r.store.CreateDashboard(in.Name, in.Description)
	if err != nil {
		respondErr(w, req, 500, err.Error())
		return
	}
	respond(w, x, 1, 1)
}
func (r *Router) getDashboard(w http.ResponseWriter, req *http.Request) {
	x, err := r.store.GetDashboard(chi.URLParam(req, "id"))
	if err != nil {
		respondErr(w, req, 404, "dashboard not found")
		return
	}
	respond(w, x, 1, 1)
}
func (r *Router) exportDashboardConfig(w http.ResponseWriter, req *http.Request) {
	x, err := r.store.GetDashboard(chi.URLParam(req, "id"))
	if err != nil {
		respondErr(w, req, 404, "dashboard not found")
		return
	}
	data, err := dashboardconfig.Marshal(x)
	if err != nil {
		respondErr(w, req, 500, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/yaml; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}
func (r *Router) importDashboardConfig(w http.ResponseWriter, req *http.Request) {
	data, err := io.ReadAll(http.MaxBytesReader(w, req.Body, 1<<20))
	if err != nil {
		respondErr(w, req, 400, "read dashboard YAML: "+err.Error())
		return
	}
	definition, err := dashboardconfig.Parse(data)
	if err != nil {
		respondErr(w, req, 400, "invalid dashboard YAML: "+err.Error())
		return
	}
	dashboard, err := definition.Dashboard(uuid.NewString())
	if err != nil {
		respondErr(w, req, 400, "invalid dashboard YAML: "+err.Error())
		return
	}
	for _, panel := range dashboard.Panels {
		if err := storage.ValidateReadOnlySQL(panel.QuerySQL); err != nil {
			respondErr(w, req, 400, fmt.Sprintf("panel %q: %v", panel.Title, err))
			return
		}
	}
	if err := r.store.ReplaceDashboardDefinition(dashboard); err != nil {
		respondErr(w, req, 500, err.Error())
		return
	}
	respond(w, dashboard, 1, 1)
}
func (r *Router) patchDashboard(w http.ResponseWriter, req *http.Request) {
	x, err := r.store.GetDashboard(chi.URLParam(req, "id"))
	if err != nil {
		respondErr(w, req, 404, "dashboard not found")
		return
	}
	var in dashboardInput
	if !decodeAndValidate(w, req, &in) {
		return
	}
	x.Name, x.Description = in.Name, in.Description
	if err = r.store.UpdateDashboard(x); err != nil {
		respondErr(w, req, 500, err.Error())
		return
	}
	respond(w, x, 1, 1)
}
func (r *Router) deleteDashboard(w http.ResponseWriter, req *http.Request) {
	if err := r.store.DeleteDashboard(chi.URLParam(req, "id")); err != nil {
		respondErr(w, req, 500, err.Error())
		return
	}
	respond(w, map[string]bool{"ok": true}, 1, 1)
}

func (r *Router) savePanel(w http.ResponseWriter, req *http.Request, update bool) {
	var in panelInput
	if !decodeAndValidate(w, req, &in) {
		return
	}
	id := chi.URLParam(req, "id")
	if _, err := r.store.GetDashboard(id); err != nil {
		respondErr(w, req, 404, "dashboard not found")
		return
	}
	if err := storage.ValidateReadOnlySQL(in.QuerySQL); err != nil {
		respondErr(w, req, 400, err.Error())
		return
	}
	p := &storage.DashboardPanel{DashboardID: id, Title: in.Title, DisplayType: in.DisplayType, QuerySQL: in.QuerySQL, QueryVersion: dashboardQueryVersion, SettingsJSON: or(in.SettingsJSON, "{}"), LayoutJSON: or(in.LayoutJSON, "{}"), Position: in.Position}
	var err error
	if update {
		p.ID = chi.URLParam(req, "panelId")
		err = r.store.UpdateDashboardPanel(p)
	} else {
		err = r.store.CreateDashboardPanel(p)
	}
	if err != nil {
		respondErr(w, req, 500, err.Error())
		return
	}
	respond(w, p, 1, 1)
}

type dslError struct{ s string }

func (e *dslError) Error() string { return e.s }
func or(a, b string) string {
	if a == "" {
		return b
	}
	return a
}
func (r *Router) saveVariable(w http.ResponseWriter, req *http.Request) {
	var in variableInput
	if !decodeAndValidate(w, req, &in) {
		return
	}
	in.Name = strings.TrimPrefix(in.Name, "$")
	v := &storage.DashboardVariable{DashboardID: chi.URLParam(req, "id"), Name: in.Name, Kind: in.Kind, Source: in.Source, OptionsJSON: or(in.OptionsJSON, "[]"), DefaultValue: in.DefaultValue}
	if err := validateDashboardVariable(v, v.DefaultValue); err != nil {
		respondErr(w, req, 400, err.Error())
		return
	}
	if err := r.store.SaveDashboardVariable(v); err != nil {
		respondErr(w, req, 500, err.Error())
		return
	}
	respond(w, v, 1, 1)
}
func validateDashboardVariable(variable *storage.DashboardVariable, value string) error {
	if value == "" {
		return nil
	}
	switch variable.Kind {
	case "number":
		if _, err := strconv.ParseFloat(value, 64); err != nil {
			return &dslError{"variable requires a number"}
		}
	case "boolean":
		if _, err := strconv.ParseBool(value); err != nil {
			return &dslError{"variable requires true or false"}
		}
	case "duration":
		if _, err := time.ParseDuration(value); err != nil {
			return &dslError{"variable requires a duration such as 500ms or 5m"}
		}
	case "enum":
		var options []string
		if err := json.Unmarshal([]byte(variable.OptionsJSON), &options); err != nil {
			return &dslError{"variable options must be valid JSON"}
		}
		for _, option := range options {
			if option == value {
				return nil
			}
		}
		return &dslError{"variable default must be one of its enum options"}
	}
	return nil
}
func (r *Router) deletePanel(w http.ResponseWriter, req *http.Request) {
	if err := r.store.DeleteDashboardPanel(chi.URLParam(req, "id"), chi.URLParam(req, "panelId")); err != nil {
		respondErr(w, req, 500, err.Error())
		return
	}
	respond(w, map[string]bool{"ok": true}, 1, 1)
}
func (r *Router) deleteVariable(w http.ResponseWriter, req *http.Request) {
	if err := r.store.DeleteDashboardVariable(chi.URLParam(req, "id"), chi.URLParam(req, "name")); err != nil {
		respondErr(w, req, 500, err.Error())
		return
	}
	respond(w, map[string]bool{"ok": true}, 1, 1)
}

func (r *Router) previewDashboardQuery(w http.ResponseWriter, req *http.Request) {
	var in struct {
		QuerySQL    string            `json:"query_sql" validate:"required,max=16000"`
		Name        string            `json:"name" validate:"omitempty,max=200"`
		DisplayType string            `json:"display_type" validate:"omitempty,oneof=single_value time_series table heatmap entity_list trace_list span_list log_list deploy_correlation"`
		Variables   map[string]string `json:"variables"`
	}
	if !decodeAndValidate(w, req, &in) {
		return
	}
	dashboard, err := r.store.GetDashboard(chi.URLParam(req, "id"))
	if err != nil {
		respondErr(w, req, 404, "dashboard not found")
		return
	}
	if err = storage.ValidateReadOnlySQL(in.QuerySQL); err != nil {
		respondErr(w, req, 400, err.Error())
		return
	}
	values := map[string]string{}
	for _, v := range dashboard.Variables {
		values[v.Name] = v.DefaultValue
	}
	for name, value := range in.Variables {
		for _, v := range dashboard.Variables {
			if v.Name == name {
				if err := validateDashboardVariable(v, value); err != nil {
					respondErr(w, req, 400, err.Error())
					return
				}
			}
		}
		values[name] = value
	}
	args := make([]any, 0, len(values))
	for name, value := range values {
		args = append(args, sql.Named(name, value))
	}
	ctx, cancel := context.WithTimeout(req.Context(), 30*time.Second)
	defer cancel()
	ctx = storage.WithQueryName(ctx, in.Name)
	columns, valuesRows, truncated, err := r.store.ReadOnlyQueryArgs(ctx, in.QuerySQL, args, 1000)
	if err != nil {
		respondErr(w, req, 400, err.Error())
		return
	}
	if err := validatePanelResult(in.DisplayType, columns); err != nil {
		respondErr(w, req, 400, err.Error())
		return
	}
	respond(w, map[string]any{"columns": columns, "rows": rowsForColumns(columns, valuesRows), "truncated": truncated, "query_version": dashboardQueryVersion}, len(valuesRows), 1)
}

func validatePanelResult(display string, columns []string) error {
	if display == "" || display == "table" {
		return nil
	}
	has := func(names ...string) bool {
		for _, candidate := range names {
			for _, column := range columns {
				if strings.EqualFold(column, candidate) {
					return true
				}
			}
		}
		return false
	}
	switch display {
	case "single_value":
		if !has("value") {
			return &dslError{"single value panels require a value column"}
		}
	case "time_series", "deploy_correlation":
		if !has("value") || !has("timestamp", "timestamp_ns", "time_ns") {
			return &dslError{"time series panels require timestamp and value columns"}
		}
	case "heatmap":
		if !has("value") || !has("x", "group_value") {
			return &dslError{"heatmap panels require value and x (or group_value) columns"}
		}
	case "entity_list":
		if !has("label", "service_name", "name") || !has("primary_value", "value", "duration_ns") {
			return &dslError{"entity list panels require a label and primary_value (or value) column"}
		}
	case "trace_list":
		if !has("trace_id") {
			return &dslError{"trace list panels require a trace_id column"}
		}
	case "span_list":
		if !has("span_id", "trace_id") {
			return &dslError{"span list panels require a span_id or trace_id column"}
		}
	case "log_list":
		if !has("body", "message") {
			return &dslError{"log list panels require a body or message column"}
		}
	}
	return nil
}
func rowsForColumns(columns []string, values [][]any) []map[string]any {
	out := make([]map[string]any, 0, len(values))
	for _, valueRow := range values {
		row := make(map[string]any, len(columns))
		for i, column := range columns {
			if i < len(valueRow) {
				row[column] = valueRow[i]
			}
		}
		out = append(out, row)
	}
	return out
}

func (r *Router) queryCatalog(w http.ResponseWriter, req *http.Request) {
	signal, search := req.URL.Query().Get("signal"), strings.TrimSpace(req.URL.Query().Get("q"))
	if len(search) > 256 {
		respondErr(w, req, 400, "search must be at most 256 bytes")
		return
	}
	if signal != "" && signal != "metrics" && signal != "spans" && signal != "traces" && signal != "logs" {
		respondErr(w, req, 400, "unknown telemetry signal")
		return
	}
	ctx, cancel := context.WithTimeout(req.Context(), 10*time.Second)
	defer cancel()
	items, err := r.store.QueryCatalog(ctx, signal, search, r.scopeSession(req.URL.Query().Get("sessionId")))
	if err != nil {
		respondErr(w, req, 500, "telemetry search failed: "+err.Error())
		return
	}
	respond(w, items, len(items), 1)
}

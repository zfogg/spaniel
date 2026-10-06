package api

import (
	"encoding/json"
	"github.com/go-chi/chi/v5"
	"github.com/zfogg/spaniel/internal/querydsl"
	"github.com/zfogg/spaniel/internal/storage"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type dashboardInput struct {
	Name        string `json:"name" validate:"required,max=120"`
	Description string `json:"description" validate:"max=1000"`
}
type panelInput struct {
	Title        string `json:"title" validate:"required,max=160"`
	DisplayType  string `json:"display_type" validate:"required,oneof=single_value time_series table heatmap trace_list log_list"`
	QueryText    string `json:"query_text" validate:"required,max=2000"`
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

func (r *Router) listDashboards(w http.ResponseWriter, q *http.Request) {
	xs, e := r.store.WithContext(q.Context()).ListDashboards()
	if e != nil {
		respondErr(w, q, 500, e.Error())
		return
	}
	respond(w, xs, len(xs), 1)
}
func (r *Router) createDashboard(w http.ResponseWriter, q *http.Request) {
	var in dashboardInput
	if !decodeAndValidate(w, q, &in) {
		return
	}
	x, e := r.store.CreateDashboard(in.Name, in.Description)
	if e != nil {
		respondErr(w, q, 500, e.Error())
		return
	}
	respond(w, x, 1, 1)
}
func (r *Router) getDashboard(w http.ResponseWriter, q *http.Request) {
	x, e := r.store.GetDashboard(chi.URLParam(q, "id"))
	if e != nil {
		respondErr(w, q, 404, "dashboard not found")
		return
	}
	respond(w, x, 1, 1)
}
func (r *Router) patchDashboard(w http.ResponseWriter, q *http.Request) {
	x, e := r.store.GetDashboard(chi.URLParam(q, "id"))
	if e != nil {
		respondErr(w, q, 404, "dashboard not found")
		return
	}
	var in dashboardInput
	if !decodeAndValidate(w, q, &in) {
		return
	}
	x.Name = in.Name
	x.Description = in.Description
	if e = r.store.UpdateDashboard(x); e != nil {
		respondErr(w, q, 500, e.Error())
		return
	}
	respond(w, x, 1, 1)
}
func (r *Router) deleteDashboard(w http.ResponseWriter, q *http.Request) {
	if e := r.store.DeleteDashboard(chi.URLParam(q, "id")); e != nil {
		respondErr(w, q, 500, e.Error())
		return
	}
	respond(w, map[string]bool{"ok": true}, 1, 1)
}
func displayCompatible(display string, q querydsl.Query) bool {
	if display == "trace_list" {
		return q.Signal == "traces"
	}
	if display == "log_list" {
		return q.Signal == "logs"
	}
	if display == "heatmap" {
		return q.Function == "heatmap"
	}
	if display == "single_value" {
		return q.GroupBy == "" && q.Function != "records"
	}
	return true
}
func dashboardVariableNames(x *storage.Dashboard) map[string]bool {
	names := make(map[string]bool, len(x.Variables))
	for _, variable := range x.Variables {
		names[variable.Name] = true
	}
	return names
}
func (r *Router) savePanel(w http.ResponseWriter, req *http.Request, update bool) {
	id := chi.URLParam(req, "id")
	var in panelInput
	if !decodeAndValidate(w, req, &in) {
		return
	}
	dashboard, e := r.store.GetDashboard(id)
	if e != nil {
		respondErr(w, req, 404, "dashboard not found")
		return
	}
	q, e := querydsl.ParseWithVariables(in.QueryText, dashboardVariableNames(dashboard))
	if e != nil || !displayCompatible(in.DisplayType, q) {
		if e == nil {
			e = &dslError{"display type does not match query result"}
		}
		respondErr(w, req, 400, e.Error())
		return
	}
	p := &storage.DashboardPanel{DashboardID: id, Title: in.Title, DisplayType: in.DisplayType, QueryText: in.QueryText, QueryJSON: q.JSON(), SettingsJSON: or(in.SettingsJSON, "{}"), LayoutJSON: or(in.LayoutJSON, "{}"), Position: in.Position}
	if update {
		p.ID = chi.URLParam(req, "panelId")
		e = r.store.UpdateDashboardPanel(p)
	} else {
		e = r.store.CreateDashboardPanel(p)
	}
	if e != nil {
		respondErr(w, req, 500, e.Error())
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
	if strings.HasPrefix(in.Name, "$") {
		in.Name = strings.TrimPrefix(in.Name, "$")
	}
	if querydsl.IsMagicVariable(in.Name) {
		respondErr(w, req, 400, "magic variables cannot be redefined")
		return
	}
	v := &storage.DashboardVariable{DashboardID: chi.URLParam(req, "id"), Name: in.Name, Kind: in.Kind, Source: in.Source, OptionsJSON: or(in.OptionsJSON, "[]"), DefaultValue: in.DefaultValue}
	if err := validateDashboardVariable(v, v.DefaultValue); err != nil {
		respondErr(w, req, 400, err.Error())
		return
	}
	if e := r.store.SaveDashboardVariable(v); e != nil {
		respondErr(w, req, 500, e.Error())
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
	if e := r.store.DeleteDashboardPanel(chi.URLParam(req, "id"), chi.URLParam(req, "panelId")); e != nil {
		respondErr(w, req, 500, e.Error())
		return
	}
	respond(w, map[string]bool{"ok": true}, 1, 1)
}
func (r *Router) deleteVariable(w http.ResponseWriter, req *http.Request) {
	if e := r.store.DeleteDashboardVariable(chi.URLParam(req, "id"), chi.URLParam(req, "name")); e != nil {
		respondErr(w, req, 500, e.Error())
		return
	}
	respond(w, map[string]bool{"ok": true}, 1, 1)
}
func (r *Router) previewDashboardQuery(w http.ResponseWriter, req *http.Request) {
	var in struct {
		QueryText   string            `json:"query_text" validate:"required,max=2000"`
		DisplayType string            `json:"display_type"`
		Variables   map[string]string `json:"variables"`
	}
	if !decodeAndValidate(w, req, &in) {
		return
	}
	dashboard, e := r.store.GetDashboard(chi.URLParam(req, "id"))
	if e != nil {
		respondErr(w, req, 404, "dashboard not found")
		return
	}
	q, e := querydsl.ParseWithVariables(in.QueryText, dashboardVariableNames(dashboard))
	if e != nil {
		respondErr(w, req, 400, e.Error())
		return
	}
	if in.DisplayType != "" && !displayCompatible(in.DisplayType, q) {
		respondErr(w, req, 400, "display type does not match query result")
		return
	}
	variables := make(map[string]string, len(dashboard.Variables)+len(in.Variables))
	for _, variable := range dashboard.Variables {
		variables[variable.Name] = variable.DefaultValue
	}
	for name, value := range in.Variables {
		for _, variable := range dashboard.Variables {
			if variable.Name == name {
				if err := validateDashboardVariable(variable, value); err != nil {
					respondErr(w, req, 400, err.Error())
					return
				}
			}
		}
		variables[name] = value
	}
	c, e := querydsl.Compile(q, variables, r.scopeSession(req.URL.Query().Get("sessionId")))
	if e != nil {
		respondErr(w, req, 400, e.Error())
		return
	}
	rows, e := r.store.WithContext(req.Context()).DashboardRows(c.SQL, c.Args...)
	if e != nil {
		respondErr(w, req, 500, e.Error())
		return
	}
	respond(w, map[string]any{"display_type": in.DisplayType, "columns": c.Columns, "rows": rows, "query": q, "warnings": []string{}}, len(rows), 1)
}
func (r *Router) queryCatalog(w http.ResponseWriter, req *http.Request) {
	signal := req.URL.Query().Get("signal")
	q := strings.ToLower(req.URL.Query().Get("q"))
	items := []map[string]string{}
	add := func(item map[string]string) {
		if q == "" || strings.Contains(strings.ToLower(item["name"]+" "+item["query"]), q) {
			items = append(items, item)
		}
	}
	if signal == "" || signal == "metrics" {
		xs, _ := r.store.ListMetricCatalog(r.scopeSession(req.URL.Query().Get("sessionId")))
		for _, x := range xs {
			add(map[string]string{"signal": "metrics", "name": x.Name, "query": "avg(" + x.Name + ")", "display_type": "time_series"})
		}
	}
	// These are bounded, valid DSL starting points. The catalog deliberately
	// exposes fields rather than interpolating arbitrary discovered values into
	// SQL, while the editor can still filter against bounded attributes.
	for _, item := range []map[string]string{
		{"signal": "spans", "name": "Span count", "query": "count(spans) by service_name", "display_type": "time_series"},
		{"signal": "spans", "name": "p95 span duration", "query": "p95(duration) by service_name", "display_type": "time_series"},
		{"signal": "spans", "name": "Span records", "query": "spans", "display_type": "table"},
		{"signal": "traces", "name": "Trace count", "query": "count(traces) by service_name", "display_type": "table"},
		{"signal": "traces", "name": "Trace records", "query": "traces", "display_type": "trace_list"},
		{"signal": "logs", "name": "Log count", "query": "count(logs) by service_name", "display_type": "time_series"},
		{"signal": "logs", "name": "Log records", "query": "logs", "display_type": "log_list"},
		{"signal": "attributes", "name": "Span attribute", "query": "count(spans) by attributes.http.method", "display_type": "table"},
		{"signal": "attributes", "name": "Resource attribute", "query": "count(spans) by resource.service.name", "display_type": "table"},
	} {
		if signal == "" || signal == item["signal"] {
			add(item)
		}
	}
	respond(w, items, len(items), 1)
}

var _ = json.RawMessage{}

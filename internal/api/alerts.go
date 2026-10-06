package api

import (
	"encoding/json"
	"github.com/go-chi/chi/v5"
	"github.com/zfogg/spaniel/internal/storage"
	"net/http"
	"time"
)

type alertInput struct {
	Name         string            `json:"name" validate:"required,max=120"`
	QuerySQL     string            `json:"query_sql" validate:"required,max=16000"`
	Condition    map[string]any    `json:"condition"`
	GroupBy      []string          `json:"group_by"`
	PendingForNs int64             `json:"pending_for_ns"`
	CooldownNs   int64             `json:"cooldown_ns"`
	Severity     string            `json:"severity" validate:"omitempty,oneof=info warning critical"`
	Enabled      *bool             `json:"enabled"`
	Annotations  map[string]string `json:"annotations"`
}

func alertModel(in alertInput) (*storage.AlertRule, error) {
	if e := storage.ValidateReadOnlySQL(in.QuerySQL); e != nil {
		return nil, e
	}
	var condition alertCondition
	conditionJSON, _ := json.Marshal(in.Condition)
	if err := json.Unmarshal(conditionJSON, &condition); err != nil || condition.Kind != "threshold" || !validAlertOperator(condition.Operator) {
		return nil, &dslError{"alerts currently support a threshold condition with a valid comparison operator"}
	}
	c, _ := json.Marshal(in.Condition)
	g, _ := json.Marshal(in.GroupBy)
	a, _ := json.Marshal(in.Annotations)
	sev := in.Severity
	if sev == "" {
		sev = "warning"
	}
	on := true
	if in.Enabled != nil {
		on = *in.Enabled
	}
	return &storage.AlertRule{Name: in.Name, QuerySQL: in.QuerySQL, QueryVersion: dashboardQueryVersion, ConditionJSON: string(c), GroupByJSON: string(g), PendingForNs: in.PendingForNs, CooldownNs: in.CooldownNs, Severity: sev, Enabled: on, AnnotationsJSON: string(a)}, nil
}
func (r *Router) listAlerts(w http.ResponseWriter, q *http.Request) {
	x, e := r.store.WithContext(q.Context()).ListAlertRules()
	if e != nil {
		respondErr(w, q, 500, e.Error())
		return
	}
	respond(w, x, len(x), 1)
}
func (r *Router) getAlert(w http.ResponseWriter, q *http.Request) {
	x, e := r.store.WithContext(q.Context()).GetAlertRule(chi.URLParam(q, "id"))
	if e != nil {
		respondErr(w, q, 404, "alert not found")
		return
	}
	respond(w, x, 1, 1)
}
func (r *Router) createAlert(w http.ResponseWriter, q *http.Request) {
	var in alertInput
	if !decodeAndValidate(w, q, &in) {
		return
	}
	x, e := alertModel(in)
	if e != nil {
		respondErr(w, q, 400, e.Error())
		return
	}
	if e = r.store.WithContext(q.Context()).CreateAlertRule(x); e != nil {
		respondErr(w, q, 500, e.Error())
		return
	}
	respond(w, x, 1, 1)
}
func (r *Router) patchAlert(w http.ResponseWriter, q *http.Request) {
	var in alertInput
	if !decodeAndValidate(w, q, &in) {
		return
	}
	x, e := alertModel(in)
	if e != nil {
		respondErr(w, q, 400, e.Error())
		return
	}
	x.ID = chi.URLParam(q, "id")
	if e = r.store.WithContext(q.Context()).UpdateAlertRule(x); e != nil {
		respondErr(w, q, 500, e.Error())
		return
	}
	respond(w, x, 1, 1)
}
func (r *Router) deleteAlert(w http.ResponseWriter, q *http.Request) {
	if e := r.store.WithContext(q.Context()).DeleteAlertRule(chi.URLParam(q, "id")); e != nil {
		respondErr(w, q, 500, e.Error())
		return
	}
	respond(w, map[string]bool{"ok": true}, 1, 1)
}
func (r *Router) acknowledgeAlert(w http.ResponseWriter, q *http.Request) {
	store := r.store.WithContext(q.Context())
	id := chi.URLParam(q, "id")
	changed, e := store.AcknowledgeAlert(id)
	if e != nil {
		respondErr(w, q, 500, e.Error())
		return
	}
	if rule, err := store.GetAlertRule(id); err == nil && r.hub != nil {
		for _, instance := range changed {
			r.hub.Broadcast(alertEvent(rule, instance, "acknowledged", time.Now()))
		}
	}
	respond(w, map[string]bool{"ok": true}, 1, 1)
}
func (r *Router) previewAlert(w http.ResponseWriter, q *http.Request) {
	store := r.store.WithContext(q.Context())
	x, e := store.GetAlertRule(chi.URLParam(q, "id"))
	if e != nil {
		respondErr(w, q, 404, "alert not found")
		return
	}
	cols, values, _, e := store.ReadOnlyQuery(q.Context(), x.QuerySQL, 1000)
	if e != nil {
		respondErr(w, q, 500, e.Error())
		return
	}
	if e := validateAlertColumns(cols, alertGroupColumns(x.GroupByJSON)); e != nil {
		respondErr(w, q, 400, e.Error())
		return
	}
	rows := rowsForColumns(cols, values)
	respond(w, map[string]any{"columns": cols, "rows": rows, "condition": json.RawMessage(x.ConditionJSON)}, len(rows), 1)
}

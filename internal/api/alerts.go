package api

import (
	"encoding/json"
	"github.com/go-chi/chi/v5"
	"github.com/zfogg/spaniel/internal/querydsl"
	"github.com/zfogg/spaniel/internal/storage"
	"net/http"
	"time"
)

type alertInput struct {
	Name         string            `json:"name" validate:"required,max=120"`
	QueryText    string            `json:"query_text" validate:"required,max=2000"`
	Condition    map[string]any    `json:"condition"`
	GroupBy      []string          `json:"group_by"`
	PendingForNs int64             `json:"pending_for_ns"`
	CooldownNs   int64             `json:"cooldown_ns"`
	Severity     string            `json:"severity" validate:"omitempty,oneof=info warning critical"`
	Enabled      *bool             `json:"enabled"`
	Annotations  map[string]string `json:"annotations"`
}

func alertModel(in alertInput) (*storage.AlertRule, error) {
	q, e := querydsl.Parse(in.QueryText)
	if e != nil {
		return nil, e
	}
	if hasMagicVariables(q) {
		return nil, &dslError{"scheduled alerts cannot use context-sensitive magic variables; use fixed filters or dashboard variables"}
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
	return &storage.AlertRule{Name: in.Name, QueryJSON: q.JSON(), ConditionJSON: string(c), GroupByJSON: string(g), PendingForNs: in.PendingForNs, CooldownNs: in.CooldownNs, Severity: sev, Enabled: on, AnnotationsJSON: string(a)}, nil
}

func hasMagicVariables(q querydsl.Query) bool {
	for _, filter := range q.Filters {
		if querydsl.IsMagicVariable(filter.Value) {
			return true
		}
	}
	return false
}
func (r *Router) listAlerts(w http.ResponseWriter, q *http.Request) {
	x, e := r.store.ListAlertRules()
	if e != nil {
		respondErr(w, q, 500, e.Error())
		return
	}
	respond(w, x, len(x), 1)
}
func (r *Router) getAlert(w http.ResponseWriter, q *http.Request) {
	x, e := r.store.GetAlertRule(chi.URLParam(q, "id"))
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
	if e = r.store.CreateAlertRule(x); e != nil {
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
	if e = r.store.UpdateAlertRule(x); e != nil {
		respondErr(w, q, 500, e.Error())
		return
	}
	respond(w, x, 1, 1)
}
func (r *Router) deleteAlert(w http.ResponseWriter, q *http.Request) {
	if e := r.store.DeleteAlertRule(chi.URLParam(q, "id")); e != nil {
		respondErr(w, q, 500, e.Error())
		return
	}
	respond(w, map[string]bool{"ok": true}, 1, 1)
}
func (r *Router) acknowledgeAlert(w http.ResponseWriter, q *http.Request) {
	id := chi.URLParam(q, "id")
	if e := r.store.AcknowledgeAlert(id); e != nil {
		respondErr(w, q, 500, e.Error())
		return
	}
	if rule, err := r.store.GetAlertRule(id); err == nil && r.hub != nil {
		for _, instance := range rule.Instances {
			if instance.AcknowledgedAt != nil {
				r.hub.Broadcast(alertEvent(rule, instance, "acknowledged", time.Now()))
			}
		}
	}
	respond(w, map[string]bool{"ok": true}, 1, 1)
}
func (r *Router) previewAlert(w http.ResponseWriter, q *http.Request) {
	x, e := r.store.GetAlertRule(chi.URLParam(q, "id"))
	if e != nil {
		respondErr(w, q, 404, "alert not found")
		return
	}
	var ast querydsl.Query
	if e = json.Unmarshal([]byte(x.QueryJSON), &ast); e != nil {
		respondErr(w, q, 400, e.Error())
		return
	}
	c, e := querydsl.Compile(ast, nil, r.scopeSession(q.URL.Query().Get("sessionId")))
	if e != nil {
		respondErr(w, q, 400, e.Error())
		return
	}
	rows, e := r.store.DashboardRows(c.SQL, c.Args...)
	if e != nil {
		respondErr(w, q, 500, e.Error())
		return
	}
	respond(w, map[string]any{"columns": c.Columns, "rows": rows, "condition": json.RawMessage(x.ConditionJSON)}, len(rows), 1)
}

package api

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/zfogg/spaniel/internal/storage"
	"github.com/zfogg/spaniel/internal/ws"
)

type Router struct {
	store *storage.DB
	hub   *ws.Hub
}

func NewRouter(store *storage.DB, hub *ws.Hub) http.Handler {
	r := &Router{store: store, hub: hub}
	mux := chi.NewRouter()
	mux.Use(middleware.Logger)
	mux.Use(middleware.Recoverer)
	mux.Use(corsMiddleware)

	mux.Get("/api/health", r.health)
	mux.Get("/api/traces", r.listTraces)
	mux.Get("/api/traces/{traceId}", r.getTrace)
	mux.Get("/api/spans/{spanId}", r.getSpan)
	mux.Get("/api/logs", r.listLogs)
	mux.Get("/api/services", r.listServices)
	mux.Get("/api/sessions", r.listSessions)
	mux.Post("/api/sessions", r.createSession)
	mux.Get("/api/sessions/active", r.getActiveSession)
	mux.Get("/api/sessions/{sessionId}", r.getSession)
	mux.Post("/api/sessions/{sessionId}/activate", r.activateSession)
	mux.Post("/api/sessions/{sessionId}/baseline", r.baselineSession)
	mux.Delete("/api/sessions/{sessionId}", r.deleteSession)
	mux.Get("/api/lint", r.listLint)
	mux.Get("/api/stats", r.getStats)
	mux.Get("/api/service-map", r.getServiceMap)
	mux.Get("/api/issues", r.getIssues)
	mux.Get("/ws", hub.ServeWS)

	return mux
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func respond(w http.ResponseWriter, data any, total, page int) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{ //nolint:errcheck
		"data": data,
		"meta": map[string]any{"total": total, "page": page},
	})
}

func respondErr(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]any{"error": msg}) //nolint:errcheck
}

func (r *Router) health(w http.ResponseWriter, _ *http.Request) {
	respond(w, map[string]bool{"ok": true}, 1, 1)
}

func (r *Router) listTraces(w http.ResponseWriter, req *http.Request) {
	q := req.URL.Query()
	sessionID := q.Get("sessionId")
	service := q.Get("service")
	limit, _ := strconv.Atoi(q.Get("limit"))
	page, _ := strconv.Atoi(q.Get("page"))
	if page < 1 {
		page = 1
	}

	traces, err := r.store.ListTraces(storage.TraceFilter{
		SessionID: sessionID,
		Service:   service,
		Limit:     limit,
		Page:      page,
	})
	if err != nil {
		respondErr(w, 500, err.Error())
		return
	}
	if traces == nil {
		traces = []*storage.TraceRow{}
	}
	respond(w, traces, len(traces), page)
}

func (r *Router) getTrace(w http.ResponseWriter, req *http.Request) {
	traceID := chi.URLParam(req, "traceId")
	spans, err := r.store.GetTrace(traceID)
	if err != nil {
		respondErr(w, 500, err.Error())
		return
	}
	if spans == nil {
		spans = []*storage.Span{}
	}
	respond(w, spans, len(spans), 1)
}

func (r *Router) getSpan(w http.ResponseWriter, req *http.Request) {
	spanID := chi.URLParam(req, "spanId")
	span, err := r.store.GetSpan(spanID)
	if err != nil {
		respondErr(w, 500, err.Error())
		return
	}
	if span == nil {
		respondErr(w, 404, "span not found")
		return
	}
	respond(w, span, 1, 1)
}

func (r *Router) listLogs(w http.ResponseWriter, req *http.Request) {
	q := req.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	page, _ := strconv.Atoi(q.Get("page"))
	if page < 1 {
		page = 1
	}

	logs, err := r.store.ListLogs(storage.LogFilter{
		SessionID: q.Get("sessionId"),
		TraceID:   q.Get("traceId"),
		SpanID:    q.Get("spanId"),
		Limit:     limit,
		Page:      page,
	})
	if err != nil {
		respondErr(w, 500, err.Error())
		return
	}
	if logs == nil {
		logs = []*storage.Log{}
	}
	respond(w, logs, len(logs), page)
}

func (r *Router) listServices(w http.ResponseWriter, _ *http.Request) {
	services, err := r.store.ListServices()
	if err != nil {
		respondErr(w, 500, err.Error())
		return
	}
	if services == nil {
		services = []string{}
	}
	respond(w, services, len(services), 1)
}

func (r *Router) listSessions(w http.ResponseWriter, _ *http.Request) {
	sessions, err := r.store.ListSessions()
	if err != nil {
		respondErr(w, 500, err.Error())
		return
	}
	if sessions == nil {
		sessions = []*storage.Session{}
	}
	respond(w, sessions, len(sessions), 1)
}

func (r *Router) createSession(w http.ResponseWriter, req *http.Request) {
	var body struct {
		Label      string `json:"label"`
		IsBaseline bool   `json:"is_baseline"`
	}
	json.NewDecoder(req.Body).Decode(&body) //nolint:errcheck
	sess, err := r.store.CreateSession(body.Label, body.IsBaseline)
	if err != nil {
		respondErr(w, 500, err.Error())
		return
	}
	respond(w, sess, 1, 1)
}

func (r *Router) getSession(w http.ResponseWriter, req *http.Request) {
	sessionID := chi.URLParam(req, "sessionId")
	sess, err := r.store.GetSession(sessionID)
	if err != nil {
		respondErr(w, 500, err.Error())
		return
	}
	if sess == nil {
		respondErr(w, 404, "session not found")
		return
	}
	respond(w, sess, 1, 1)
}

func (r *Router) getActiveSession(w http.ResponseWriter, _ *http.Request) {
	respond(w, map[string]string{
		"id":    r.store.ActiveSessionID(),
		"label": r.store.ActiveSessionLabel(),
	}, 1, 1)
}

func (r *Router) activateSession(w http.ResponseWriter, req *http.Request) {
	sessionID := chi.URLParam(req, "sessionId")
	sess, err := r.store.GetSession(sessionID)
	if err != nil {
		respondErr(w, 500, err.Error())
		return
	}
	if sess == nil {
		respondErr(w, 404, "session not found")
		return
	}
	r.store.SetActiveSession(sess.ID, sess.Label)
	respond(w, sess, 1, 1)
}

func (r *Router) baselineSession(w http.ResponseWriter, req *http.Request) {
	sessionID := chi.URLParam(req, "sessionId")
	var body struct {
		IsBaseline bool `json:"is_baseline"`
	}
	json.NewDecoder(req.Body).Decode(&body) //nolint:errcheck
	if err := r.store.SetBaseline(sessionID, body.IsBaseline); err != nil {
		respondErr(w, 500, err.Error())
		return
	}
	respond(w, map[string]bool{"ok": true}, 1, 1)
}

func (r *Router) deleteSession(w http.ResponseWriter, req *http.Request) {
	sessionID := chi.URLParam(req, "sessionId")
	if sessionID == r.store.ActiveSessionID() {
		respondErr(w, 400, "cannot delete the active session")
		return
	}
	if err := r.store.DeleteSession(sessionID); err != nil {
		respondErr(w, 500, err.Error())
		return
	}
	respond(w, map[string]bool{"ok": true}, 1, 1)
}

func (r *Router) listLint(w http.ResponseWriter, req *http.Request) {
	sessionID := req.URL.Query().Get("sessionId")
	warnings, err := r.store.ListLintWarnings(sessionID)
	if err != nil {
		respondErr(w, 500, err.Error())
		return
	}
	if warnings == nil {
		warnings = []*storage.LintWarning{}
	}
	respond(w, warnings, len(warnings), 1)
}

func (r *Router) getStats(w http.ResponseWriter, req *http.Request) {
	sessionID := req.URL.Query().Get("sessionId")
	stats, err := r.store.GetStats(sessionID)
	if err != nil {
		respondErr(w, 500, err.Error())
		return
	}
	respond(w, stats, 1, 1)
}

func (r *Router) getServiceMap(w http.ResponseWriter, req *http.Request) {
	sessionID := req.URL.Query().Get("sessionId")
	data, err := r.store.GetServiceMap(sessionID)
	if err != nil {
		respondErr(w, 500, err.Error())
		return
	}
	respond(w, data, 1, 1)
}

func (r *Router) getIssues(w http.ResponseWriter, req *http.Request) {
	traceID := req.URL.Query().Get("traceId")
	if traceID == "" {
		respondErr(w, 400, "traceId required")
		return
	}
	issues, err := r.store.GetTraceIssues(traceID)
	if err != nil {
		respondErr(w, 500, err.Error())
		return
	}
	respond(w, issues, len(issues), 1)
}

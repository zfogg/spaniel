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
	mux.Get("/api/sessions/{sessionId}", r.getSession)
	mux.Get("/api/lint", r.listLint)
	mux.Get("/api/stats", r.getStats)
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

func respond(w http.ResponseWriter, data any, total int) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{ //nolint:errcheck
		"data": data,
		"meta": map[string]any{"total": total},
	})
}

func respondErr(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]any{"error": msg}) //nolint:errcheck
}

func (r *Router) health(w http.ResponseWriter, _ *http.Request) {
	respond(w, map[string]string{"status": "ok"}, 1)
}

func (r *Router) listTraces(w http.ResponseWriter, req *http.Request) {
	sessionID := req.URL.Query().Get("sessionId")
	limit, _ := strconv.Atoi(req.URL.Query().Get("limit"))
	traces, err := r.store.ListTraces(sessionID, limit)
	if err != nil {
		respondErr(w, 500, err.Error())
		return
	}
	if traces == nil {
		traces = []*storage.TraceRow{}
	}
	respond(w, traces, len(traces))
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
	respond(w, spans, len(spans))
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
	respond(w, span, 1)
}

func (r *Router) listLogs(w http.ResponseWriter, req *http.Request) {
	sessionID := req.URL.Query().Get("sessionId")
	limit, _ := strconv.Atoi(req.URL.Query().Get("limit"))
	logs, err := r.store.ListLogs(sessionID, limit)
	if err != nil {
		respondErr(w, 500, err.Error())
		return
	}
	if logs == nil {
		logs = []*storage.Log{}
	}
	respond(w, logs, len(logs))
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
	respond(w, services, len(services))
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
	respond(w, sessions, len(sessions))
}

func (r *Router) createSession(w http.ResponseWriter, req *http.Request) {
	var body struct {
		Label string `json:"label"`
	}
	json.NewDecoder(req.Body).Decode(&body) //nolint:errcheck
	sess, err := r.store.CreateSession(body.Label)
	if err != nil {
		respondErr(w, 500, err.Error())
		return
	}
	respond(w, sess, 1)
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
	respond(w, sess, 1)
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
	respond(w, warnings, len(warnings))
}

func (r *Router) getStats(w http.ResponseWriter, req *http.Request) {
	sessionID := req.URL.Query().Get("sessionId")
	stats, err := r.store.GetStats(sessionID)
	if err != nil {
		respondErr(w, 500, err.Error())
		return
	}
	respond(w, stats, 1)
}

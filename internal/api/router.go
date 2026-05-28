package api

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/ptrace"
	"go.opentelemetry.io/collector/pdata/ptrace/ptraceotlp"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

	"github.com/zfogg/spaniel/internal/coverage"
	"github.com/zfogg/spaniel/internal/forwarder"
	"github.com/zfogg/spaniel/internal/storage"
	"github.com/zfogg/spaniel/internal/ws"
)

// ThroughputProvider returns live per-second ingest rates. Implemented by
// *ingestion.Pipeline; nil means no live rates are available.
type ThroughputProvider interface {
	Throughput() storage.Throughput
}

// DropCounterProvider returns process-level drop counters. Implemented by
// *ingestion.Pipeline via its Counters field; nil means sampling is disabled.
type DropCounterProvider interface {
	DroppedSpansTotal() int64
	DroppedLogsTotal() int64
	DroppedMetricPointsTotal() int64
	LastDropAt() int64
}

// SourcesProvider returns per-source ingest stats. Implemented by *ingestion.Pipeline.
type SourcesProvider interface {
	Sources() []storage.SourceStats
}

type Router struct {
	store     *storage.DB
	hub       *ws.Hub
	forwarder *forwarder.Forwarder // nil when no upstream configured
	manifests *coverage.Manifests  // nil when no spec file is loaded
	settings  *SettingsService     // nil disables /api/settings
	tp        ThroughputProvider   // nil when no pipeline wired in
	dc        DropCounterProvider  // nil when sampling disabled
	sp        SourcesProvider      // nil when no pipeline wired in
}

func NewRouter(store *storage.DB, hub *ws.Hub, fwd *forwarder.Forwarder) http.Handler {
	return NewRouterFull(store, hub, fwd, nil, nil, nil, nil, nil)
}

// NewRouterWithManifests is a back-compat shim — prefer NewRouterFull.
func NewRouterWithManifests(store *storage.DB, hub *ws.Hub, fwd *forwarder.Forwarder, mfs *coverage.Manifests) http.Handler {
	return NewRouterFull(store, hub, fwd, mfs, nil, nil, nil, nil)
}

// NewRouterFull is the canonical constructor. Pass nil for any optional
// dependency (manifests, settings, tp, dc, sp) to disable the corresponding feature.
func NewRouterFull(store *storage.DB, hub *ws.Hub, fwd *forwarder.Forwarder, mfs *coverage.Manifests, settings *SettingsService, tp ThroughputProvider, dc DropCounterProvider, sp SourcesProvider) http.Handler {
	r := &Router{store: store, hub: hub, forwarder: fwd, manifests: mfs, settings: settings, tp: tp, dc: dc, sp: sp}
	mux := chi.NewRouter()
	mux.Use(middleware.RequestID)
	mux.Use(middleware.Logger)
	mux.Use(middleware.Recoverer)
	mux.Use(corsMiddleware)
	mux.Use(func(next http.Handler) http.Handler {
		return otelhttp.NewHandler(next, "spaniel.api",
			otelhttp.WithSpanNameFormatter(func(_ string, r *http.Request) string {
				if rctx := chi.RouteContext(r.Context()); rctx != nil {
					if p := rctx.RoutePattern(); p != "" {
						return r.Method + " " + p
					}
				}
				return r.Method + " " + r.URL.Path
			}),
		)
	})

	mux.Get("/api/health", r.health)
	mux.Get("/api/traces", r.listTraces)
	mux.Get("/api/traces/{traceId}", r.getTrace)
	mux.Get("/api/traces/{traceId}/export", r.exportTrace)
	mux.Get("/api/traces/{traceId}/incoming-links", r.listIncomingLinks)
	mux.Get("/api/spans", r.listSpans)
	mux.Get("/api/spans/{spanId}", r.getSpan)
	mux.Get("/api/logs", r.listLogs)
	mux.Get("/api/services", r.listServices)
	mux.Get("/api/sessions", r.listSessions)
	mux.Post("/api/sessions", r.createSession)
	mux.Post("/api/sessions/import", r.importSession)
	mux.Get("/api/sessions/active", r.getActiveSession)
	mux.Get("/api/sessions/{sessionId}", r.getSession)
	mux.Patch("/api/sessions/{sessionId}", r.patchSession)
	mux.Post("/api/sessions/{sessionId}/activate", r.activateSession)
	mux.Post("/api/sessions/{sessionId}/baseline", r.baselineSession)
	mux.Delete("/api/sessions/{sessionId}", r.deleteSession)
	mux.Get("/api/lint", r.listLint)
	mux.Get("/api/stats", r.getStats)
	mux.Get("/api/service-map", r.getServiceMap)
	mux.Get("/api/issues", r.getIssues)
	mux.Get("/api/diff", r.getDiff)
	mux.Get("/api/forwarders", r.listForwarders)
	mux.Get("/api/search", r.search)
	mux.Get("/api/sessions/{sessionId}/baseline-export", r.exportBaseline)
	mux.Get("/api/metrics", r.listMetrics)
	mux.Get("/api/metrics/series", r.getMetricSeries)
	mux.Get("/api/coverage", r.getCoverage)
	mux.Get("/api/settings", r.getSettings)
	mux.Put("/api/settings", r.putSettings)
	mux.Delete("/api/settings/data", r.dropAllData)
	mux.Post("/api/settings/compact", r.compact)
	mux.Post("/api/settings/prune", r.prune)
	mux.Get("/api/storage", r.getStorageBreakdown)
	mux.Get("/api/sources", r.listSources)
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

func respondErr(w http.ResponseWriter, req *http.Request, code int, msg string) {
	reqID := middleware.GetReqID(req.Context())
	w.Header().Set("Content-Type", "application/json")
	if reqID != "" {
		w.Header().Set("X-Request-ID", reqID)
	}
	w.WriteHeader(code)
	body := map[string]any{"error": msg}
	if reqID != "" {
		body["request_id"] = reqID
	}
	json.NewEncoder(w).Encode(body) //nolint:errcheck
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
		respondErr(w, req, 500, err.Error())
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
		respondErr(w, req, 500, err.Error())
		return
	}
	if spans == nil {
		spans = []*storage.Span{}
	}
	// Bulk-attach links so the waterfall can show the link badge without a
	// per-span round-trip. Group by span_id into a map, then attach.
	if links, err := r.store.ListLinksByTrace(traceID); err == nil && len(links) > 0 {
		bySpan := make(map[string][]*storage.SpanLink, len(links))
		for _, l := range links {
			bySpan[l.SpanID] = append(bySpan[l.SpanID], l)
		}
		for _, s := range spans {
			if sl := bySpan[s.SpanID]; sl != nil {
				s.Links = sl
			} else {
				s.Links = []*storage.SpanLink{}
			}
		}
	} else {
		for _, s := range spans {
			s.Links = []*storage.SpanLink{}
		}
	}
	respond(w, spans, len(spans), 1)
}

func (r *Router) listSpans(w http.ResponseWriter, req *http.Request) {
	q := req.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	rows, err := r.store.ListSpans(storage.SpanFilter{
		SessionID: q.Get("sessionId"),
		Sort:      q.Get("sort"),
		Limit:     limit,
	})
	if err != nil {
		respondErr(w, req, 500, err.Error())
		return
	}
	if rows == nil {
		rows = []*storage.SpanRow{}
	}
	respond(w, rows, len(rows), 1)
}

func (r *Router) getSpan(w http.ResponseWriter, req *http.Request) {
	spanID := chi.URLParam(req, "spanId")
	span, err := r.store.GetSpan(spanID)
	if err != nil {
		respondErr(w, req, 500, err.Error())
		return
	}
	if span == nil {
		respondErr(w, req, 404, "span not found")
		return
	}
	// Attach the span's events so the inspector has them without a second
	// round-trip. Best-effort: a DB failure here doesn't shadow the span.
	// Always materialize the slice (even empty) so the JSON field is `[]`,
	// never `null`.
	span.Events = []*storage.SpanEvent{}
	if events, err := r.store.ListEventsBySpan(span.SpanID); err == nil && events != nil {
		span.Events = events
	}
	span.Links = []*storage.SpanLink{}
	if links, err := r.store.ListLinksBySpan(span.SpanID); err == nil && links != nil {
		span.Links = links
	}
	respond(w, span, 1, 1)
}

// listIncomingLinks returns every span_link whose linked_trace_id matches
// the trace_id URL param — the "who links into this trace?" reverse lookup.
// exportTrace renders the trace as a self-contained OTLP JSON document that
// the spaniel importer can re-import verbatim (round-trip guaranteed).
// One resourceSpans block is emitted per distinct (service_name, resource) pair.
func (r *Router) exportTrace(w http.ResponseWriter, req *http.Request) {
	traceID := chi.URLParam(req, "traceId")
	spans, err := r.store.GetTrace(traceID)
	if err != nil {
		respondErr(w, req, 500, err.Error())
		return
	}
	if len(spans) == 0 {
		respondErr(w, req, 404, "trace not found")
		return
	}

	type rsKey struct{ service, resource string }
	order := make([]rsKey, 0, len(spans))
	groups := make(map[rsKey][]*storage.Span)
	for _, s := range spans {
		k := rsKey{service: s.ServiceName, resource: s.Resource}
		if _, seen := groups[k]; !seen {
			order = append(order, k)
		}
		groups[k] = append(groups[k], s)
	}

	td := ptrace.NewTraces()
	for _, k := range order {
		rs := td.ResourceSpans().AppendEmpty()
		var resAttrs map[string]any
		_ = json.Unmarshal([]byte(k.resource), &resAttrs)
		putAttrs(rs.Resource().Attributes(), resAttrs)
		if _, ok := rs.Resource().Attributes().Get("service.name"); !ok {
			rs.Resource().Attributes().PutStr("service.name", k.service)
		}

		ss := rs.ScopeSpans().AppendEmpty()
		for _, span := range groups[k] {
			s := ss.Spans().AppendEmpty()
			s.SetTraceID(decodeTraceID(span.TraceID))
			s.SetSpanID(decodeSpanID(span.SpanID))
			s.SetParentSpanID(decodeSpanID(span.ParentSpanID))
			s.SetName(span.Name)
			s.SetKind(ptrace.SpanKind(span.Kind))
			s.SetStartTimestamp(pcommon.Timestamp(span.StartNs))
			s.SetEndTimestamp(pcommon.Timestamp(span.EndNs))
			s.Status().SetCode(ptrace.StatusCode(span.StatusCode))
			s.Status().SetMessage(span.StatusMessage)
			var attrs map[string]any
			_ = json.Unmarshal([]byte(span.Attributes), &attrs)
			putAttrs(s.Attributes(), attrs)
		}
	}

	exportReq := ptraceotlp.NewExportRequestFromTraces(td)
	jsonBytes, err := exportReq.MarshalJSON()
	if err != nil {
		respondErr(w, req, 500, err.Error())
		return
	}

	shortID := traceID
	if len(shortID) > 8 {
		shortID = shortID[:8]
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="trace-%s.json"`, shortID))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(jsonBytes)
}

func putAttrs(m pcommon.Map, raw map[string]any) {
	for k, v := range raw {
		switch vv := v.(type) {
		case string:
			m.PutStr(k, vv)
		case bool:
			m.PutBool(k, vv)
		case float64:
			m.PutDouble(k, vv)
		}
	}
}

func decodeTraceID(s string) pcommon.TraceID {
	b, _ := hex.DecodeString(s)
	var id pcommon.TraceID
	copy(id[:], b)
	return id
}

func decodeSpanID(s string) pcommon.SpanID {
	b, _ := hex.DecodeString(s)
	var id pcommon.SpanID
	copy(id[:], b)
	return id
}

func (r *Router) listIncomingLinks(w http.ResponseWriter, req *http.Request) {
	traceID := chi.URLParam(req, "traceId")
	links, err := r.store.ListIncomingLinks(traceID)
	if err != nil {
		respondErr(w, req, 500, err.Error())
		return
	}
	if links == nil {
		links = []*storage.SpanLink{}
	}
	respond(w, links, len(links), 1)
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
		respondErr(w, req, 500, err.Error())
		return
	}
	if logs == nil {
		logs = []*storage.Log{}
	}
	respond(w, logs, len(logs), page)
}

func (r *Router) listServices(w http.ResponseWriter, req *http.Request) {
	services, err := r.store.ListServices()
	if err != nil {
		respondErr(w, req, 500, err.Error())
		return
	}
	if services == nil {
		services = []string{}
	}
	respond(w, services, len(services), 1)
}

func (r *Router) listSessions(w http.ResponseWriter, req *http.Request) {
	sessions, err := r.store.ListSessions()
	if err != nil {
		respondErr(w, req, 500, err.Error())
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
		respondErr(w, req, 500, err.Error())
		return
	}
	respond(w, sess, 1, 1)
}

func (r *Router) getSession(w http.ResponseWriter, req *http.Request) {
	sessionID := chi.URLParam(req, "sessionId")
	sess, err := r.store.GetSession(sessionID)
	if err != nil {
		respondErr(w, req, 500, err.Error())
		return
	}
	if sess == nil {
		respondErr(w, req, 404, "session not found")
		return
	}
	respond(w, sess, 1, 1)
}

func (r *Router) patchSession(w http.ResponseWriter, req *http.Request) {
	sessionID := chi.URLParam(req, "sessionId")
	var body struct {
		Label *string `json:"label"`
		Note  *string `json:"note"`
	}
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		respondErr(w, req, 400, "invalid JSON")
		return
	}
	if err := r.store.UpdateSession(sessionID, storage.SessionPatch{
		Label: body.Label,
		Note:  body.Note,
	}); err != nil {
		respondErr(w, req, 500, err.Error())
		return
	}
	sess, err := r.store.GetSession(sessionID)
	if err != nil {
		respondErr(w, req, 500, err.Error())
		return
	}
	if sess == nil {
		respondErr(w, req, 404, "session not found")
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
		respondErr(w, req, 500, err.Error())
		return
	}
	if sess == nil {
		respondErr(w, req, 404, "session not found")
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
		respondErr(w, req, 500, err.Error())
		return
	}
	respond(w, map[string]bool{"ok": true}, 1, 1)
}

func (r *Router) deleteSession(w http.ResponseWriter, req *http.Request) {
	sessionID := chi.URLParam(req, "sessionId")
	if sessionID == r.store.ActiveSessionID() {
		respondErr(w, req, 400, "cannot delete the active session")
		return
	}
	if err := r.store.DeleteSession(sessionID); err != nil {
		respondErr(w, req, 500, err.Error())
		return
	}
	respond(w, map[string]bool{"ok": true}, 1, 1)
}

func (r *Router) listLint(w http.ResponseWriter, req *http.Request) {
	sessionID := req.URL.Query().Get("sessionId")
	warnings, err := r.store.ListLintWarnings(sessionID)
	if err != nil {
		respondErr(w, req, 500, err.Error())
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
		respondErr(w, req, 500, err.Error())
		return
	}
	if r.tp != nil {
		stats.Throughput = r.tp.Throughput()
	}
	if r.dc != nil {
		stats.DroppedSpans = r.dc.DroppedSpansTotal()
		stats.DroppedLogs = r.dc.DroppedLogsTotal()
		stats.DroppedMetricPoints = r.dc.DroppedMetricPointsTotal()
		stats.LastDropAt = r.dc.LastDropAt()
	}
	respond(w, stats, 1, 1)
}

func (r *Router) getServiceMap(w http.ResponseWriter, req *http.Request) {
	sessionID := req.URL.Query().Get("sessionId")
	data, err := r.store.GetServiceMap(sessionID)
	if err != nil {
		respondErr(w, req, 500, err.Error())
		return
	}
	respond(w, data, 1, 1)
}

func (r *Router) getIssues(w http.ResponseWriter, req *http.Request) {
	traceID := req.URL.Query().Get("traceId")
	if traceID == "" {
		respondErr(w, req, 400, "traceId required")
		return
	}
	issues, err := r.store.GetTraceIssues(traceID)
	if err != nil {
		respondErr(w, req, 500, err.Error())
		return
	}
	respond(w, issues, len(issues), 1)
}

func (r *Router) listSources(w http.ResponseWriter, req *http.Request) {
	if r.sp == nil {
		respond(w, []storage.SourceStats{}, 0, 1)
		return
	}
	sources := r.sp.Sources()
	if sources == nil {
		sources = []storage.SourceStats{}
	}
	respond(w, sources, len(sources), 1)
}

func (r *Router) listForwarders(w http.ResponseWriter, _ *http.Request) {
	if r.forwarder == nil {
		respond(w, []forwarder.Status{}, 0, 1)
		return
	}
	statuses := r.forwarder.Status()
	respond(w, statuses, len(statuses), 1)
}

func (r *Router) getStorageBreakdown(w http.ResponseWriter, req *http.Request) {
	bd, err := r.store.GetStorageBreakdown()
	if err != nil {
		respondErr(w, req, 500, err.Error())
		return
	}
	respond(w, bd, 1, 1)
}

func (r *Router) compact(w http.ResponseWriter, req *http.Request) {
	res, err := r.store.Compact()
	if err != nil {
		respondErr(w, req, 500, err.Error())
		return
	}
	respond(w, res, 1, 1)
}

// prune applies the retention policy immediately (POST /api/settings/prune).
// This is the one-shot the Settings page "spaniel prune" button calls, so it
// runs the same logic as the CLI `spaniel prune` / the hourly background pass.
// The active session is passed so it's never dropped.
func (r *Router) prune(w http.ResponseWriter, req *http.Request) {
	if r.settings == nil {
		respondErr(w, req, 404, "settings service not configured")
		return
	}
	v := r.settings.Viper
	cfg := storage.RetentionConfig{
		MaxSessions: v.GetInt("max_sessions"),
	}
	if days := v.GetInt("retention_days"); days > 0 {
		cfg.MaxAge = time.Duration(days) * 24 * time.Hour
	}
	if mb := v.GetInt("max_db_size_mb"); mb > 0 {
		cfg.MaxDBSizeBytes = int64(mb) * 1024 * 1024
	}
	res, err := r.store.Prune(cfg, r.store.ActiveSessionID())
	if err != nil {
		respondErr(w, req, 500, err.Error())
		return
	}
	respond(w, res, 1, 1)
}

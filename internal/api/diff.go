package api

import (
	"net/http"

	"github.com/zfogg/spaniel/internal/diff"
)

// Diff types are produced by the shared internal/diff package (also used by the
// MCP diff_sessions tool). These aliases preserve the api.* names the HTTP layer
// and the `spaniel diff` CLI already use.
type (
	DiffResult      = diff.Result
	DiffSpan        = diff.Span
	DiffSummary     = diff.Summary
	DiffSessionInfo = diff.SessionInfo
)

func (r *Router) getDiff(w http.ResponseWriter, req *http.Request) {
	q := req.URL.Query()
	baselineID := q.Get("baseline")
	compareID := q.Get("compare")

	if baselineID == "" || compareID == "" {
		respondErr(w, req, 400, "baseline and compare query params required")
		return
	}

	baseSess, err := r.store.WithContext(req.Context()).GetSession(baselineID)
	if err != nil || baseSess == nil {
		respondErr(w, req, 404, "baseline session not found")
		return
	}
	cmpSess, err := r.store.WithContext(req.Context()).GetSession(compareID)
	if err != nil || cmpSess == nil {
		respondErr(w, req, 404, "compare session not found")
		return
	}

	baseSpans, err := r.store.WithContext(req.Context()).GetSpansBySession(baselineID)
	if err != nil {
		respondErr(w, req, 500, err.Error())
		return
	}
	cmpSpans, err := r.store.WithContext(req.Context()).GetSpansBySession(compareID)
	if err != nil {
		respondErr(w, req, 500, err.Error())
		return
	}

	result := diff.Compute(baseSess, cmpSess, baseSpans, cmpSpans)
	respond(w, result, 1, 1)
}

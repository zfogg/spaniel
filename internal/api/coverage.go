package api

import (
	"net/http"

	"github.com/zfogg/spaniel/internal/coverage"
	"github.com/zfogg/spaniel/internal/storage"
)

// getCoverage returns a coverage.Report joining observed spans (optionally
// filtered to one session) against any manifests loaded at startup.
//   GET /api/coverage?sessionId=...
func (r *Router) getCoverage(w http.ResponseWriter, req *http.Request) {
	sessionID := req.URL.Query().Get("sessionId")
	var spans []*storage.Span
	var err error
	if sessionID != "" {
		spans, err = r.store.GetSpansBySession(sessionID)
	} else {
		spans, err = r.store.GetSpansBySession(r.store.ActiveSessionID())
	}
	if err != nil {
		respondErr(w, 500, err.Error())
		return
	}
	report := coverage.Compute(spans, r.manifests)
	respond(w, report, len(report.Services), 1)
}

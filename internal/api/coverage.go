package api

import (
	"net/http"

	"github.com/zfogg/spaniel/internal/coverage"
)

// getCoverage returns a coverage.Report joining observed spans (optionally
// filtered to one session) against any manifests loaded at startup.
//
//	GET /api/coverage?sessionId=...
func (r *Router) getCoverage(w http.ResponseWriter, req *http.Request) {
	sessionID := req.URL.Query().Get("sessionId")
	if sessionID == "" {
		sessionID = r.store.ActiveSessionID()
	}
	operations, err := r.store.WithContext(req.Context()).ListCoverageOperations(sessionID)
	if err != nil {
		respondErr(w, req, 500, err.Error())
		return
	}
	observed := make([]coverage.Operation, len(operations))
	for i, operation := range operations {
		observed[i] = coverage.Operation{
			ServiceName: operation.ServiceName,
			Method:      operation.Method,
			Path:        operation.Path,
			Hits:        operation.Hits,
			P95Ns:       operation.P95Ns,
		}
	}
	report := coverage.ComputeOperations(observed, r.manifests)
	respond(w, report, len(report.Services), 1)
}

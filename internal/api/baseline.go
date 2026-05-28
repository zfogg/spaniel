package api

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/zfogg/spaniel/internal/ci"
)

// exportBaseline returns a ci.Snapshot for the named session.
// Powers `spaniel ci export` and `spaniel ci check`.
func (r *Router) exportBaseline(w http.ResponseWriter, req *http.Request) {
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

	spans, err := r.store.GetSpansBySession(sessionID)
	if err != nil {
		respondErr(w, req, 500, err.Error())
		return
	}
	issues, err := r.store.ListTraceIssuesBySession(sessionID)
	if err != nil {
		respondErr(w, req, 500, err.Error())
		return
	}

	snap := ci.BuildSnapshot(sess.Label, time.Now().UnixNano(), spans, issues)
	respond(w, snap, 1, 1)
}

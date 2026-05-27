package api

import (
	"net/http"
	"strconv"

	"github.com/zfogg/spaniel/internal/storage"
)

func (r *Router) search(w http.ResponseWriter, req *http.Request) {
	q := req.URL.Query()
	query := q.Get("q")
	if query == "" {
		respond(w, []*storage.SearchResult{}, 0, 1)
		return
	}
	sessionID := q.Get("sessionId")
	limit, _ := strconv.Atoi(q.Get("limit"))

	results, err := r.store.Search(query, sessionID, limit)
	if err != nil {
		respondErr(w, 500, err.Error())
		return
	}
	if results == nil {
		results = []*storage.SearchResult{}
	}
	respond(w, results, len(results), 1)
}

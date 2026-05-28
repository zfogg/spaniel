package api

import (
	"io"
	"net/http"

	"github.com/zfogg/spaniel/internal/importer"
)

func (r *Router) importSession(w http.ResponseWriter, req *http.Request) {
	q := req.URL.Query()
	label := q.Get("label")
	format := importer.Format(q.Get("format"))
	if format == "" {
		format = importer.FormatAuto
	}

	data, err := io.ReadAll(io.LimitReader(req.Body, 64<<20)) // 64 MB max
	if err != nil {
		respondErr(w, req, 400, "read body: "+err.Error())
		return
	}
	if len(data) == 0 {
		respondErr(w, req, 400, "empty body")
		return
	}

	result, err := importer.Import(r.store, label, format, data)
	if err != nil {
		respondErr(w, req, 400, err.Error())
		return
	}

	respond(w, result, 1, 1)
}

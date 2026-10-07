package api

import (
	"encoding/json"
	"net/http"

	"github.com/zfogg/spaniel/internal/generated"
	"github.com/zfogg/spaniel/internal/storage"
)

func (r *Router) databaseSchema(w http.ResponseWriter, req *http.Request) {
	var catalog storage.SchemaCatalog
	if err := json.Unmarshal(generated.DatabaseSchema, &catalog); err != nil {
		respondErr(w, req, http.StatusInternalServerError, "embedded database schema catalog is invalid")
		return
	}
	respond(w, catalog, len(catalog.Views), 1)
}

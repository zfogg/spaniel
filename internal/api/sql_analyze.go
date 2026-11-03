package api

import (
	"net/http"

	sequel "github.com/zfogg/sequel/go"
	"github.com/zfogg/spaniel/internal/api/apigen"
	"github.com/zfogg/spaniel/internal/querydiagnostics"
)

func (r *Router) analyzeSQL(w http.ResponseWriter, req *http.Request) {
	var in apigen.AnalyzeSQLJSONRequestBody
	if !decodeAndValidate(w, req, &in) {
		return
	}
	if !in.Source.Valid() || in.Dialect != nil && !in.Dialect.Valid() {
		respondErr(w, req, http.StatusBadRequest, "Invalid SQL analysis options")
		return
	}
	dialect := sequel.DialectDuckDB
	if in.Dialect != nil {
		dialect = sequel.Dialect(*in.Dialect)
	}
	attrs := map[string]any{}
	if in.Diagnostics != nil {
		attrs[querydiagnostics.Attribute] = *in.Diagnostics
	}
	if in.SourceMap != nil {
		attrs["gotel.db.query.source_map"] = *in.SourceMap
	}
	if in.OriginalLength != nil {
		attrs["gotel.db.query.original_length"] = *in.OriginalLength
	}
	if in.DiagnosticSchemaVersion != nil {
		attrs["gotel.db.query.diagnostic_schema_version"] = *in.DiagnosticSchemaVersion
	}
	out, err := querydiagnostics.Display(req.Context(), in.Sql, dialect, in.Source == "original", attrs)
	if err != nil {
		respondErr(w, req, http.StatusBadRequest, "Invalid SQL diagnostic metadata")
		return
	}
	respond(w, out, 1, 1)
}

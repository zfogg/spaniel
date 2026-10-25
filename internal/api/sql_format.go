package api

import (
	"net/http"

	sequel "github.com/zfogg/sequel/go"
	"github.com/zfogg/spaniel/internal/api/apigen"
)

func (r *Router) formatSQL(w http.ResponseWriter, req *http.Request) {
	var in apigen.FormatSQLJSONRequestBody
	if !decodeAndValidate(w, req, &in) {
		return
	}
	dialect := sequel.DialectDuckDB
	if in.Dialect != nil {
		dialect = sequel.Dialect(*in.Dialect)
	}
	out, err := sequel.FormatSQL(dialect, in.Sql)
	if err != nil {
		respondErr(w, req, http.StatusBadRequest, err.Error())
		return
	}
	respond(w, map[string]string{"sql": out}, 1, 1)
}

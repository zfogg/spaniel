package api

import (
	"net/http"

	"github.com/zfogg/gotel/sqltrace"
	"github.com/zfogg/spaniel/internal/api/apigen"
)

func (r *Router) formatSQL(w http.ResponseWriter, req *http.Request) {
	var in apigen.FormatSQLJSONRequestBody
	if !decodeAndValidate(w, req, &in) {
		return
	}
	dialect := sqltrace.DialectDuckDB
	if in.Dialect != nil {
		dialect = sqltrace.Dialect(*in.Dialect)
	}
	out, err := sqltrace.FormatSQL(dialect, in.Sql)
	if err != nil {
		respondErr(w, req, http.StatusBadRequest, err.Error())
		return
	}
	respond(w, map[string]string{"sql": out}, 1, 1)
}

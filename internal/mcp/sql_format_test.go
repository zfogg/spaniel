package mcp

import (
	"context"
	"strings"
	"testing"

	"github.com/zfogg/gotel/sqltrace"
)

func TestFormatSQLTool(t *testing.T) {
	h := &handler{}
	_, out, err := h.formatSQL(context.Background(), nil, FormatSQLInput{SQL: "-- name\nselect id,name from t where id=$1; -- keep", Dialect: sqltrace.DialectPostgreSQL})
	if err != nil || !strings.Contains(out.SQL, "$1") || !strings.Contains(out.SQL, "-- keep") || !strings.Contains(out.SQL, "select\n") {
		t.Fatalf("%q %v", out.SQL, err)
	}
	_, _, err = h.formatSQL(context.Background(), nil, FormatSQLInput{SQL: "select 'broken"})
	if err == nil {
		t.Fatal("expected error")
	}
}

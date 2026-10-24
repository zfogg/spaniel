package mcp

import (
	"context"
	"strings"
	"testing"

	"github.com/zfogg/gotel/go/sqltrace"
	"github.com/zfogg/spaniel/internal/sqlformat"
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

func TestSearchPresentationPreservesSearchText(t *testing.T) {
	out := sqlformat.Presentation(SearchOutput{Query: "select worker", Results: []SearchHit{}}).(map[string]any)
	if out["query"] != "select worker" {
		t.Fatalf("search text formatted as SQL: %#v", out)
	}
}

func TestFormatSQLToolClickHouse(t *testing.T) {
	h := &handler{}
	_, out, err := h.formatSQL(context.Background(), nil, FormatSQLInput{SQL: "SELECT id,name FROM builds FINAL PREWHERE id={id:UInt64}", Dialect: sqltrace.DialectClickHouse})
	if err != nil || !strings.Contains(out.SQL, "{id:UInt64}") || !strings.Contains(out.SQL, "SELECT\n") {
		t.Fatalf("%q %v", out.SQL, err)
	}
}

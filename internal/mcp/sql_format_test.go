package mcp

import (
	"github.com/zfogg/spaniel/internal/sqlformat"
	"testing"
)

func TestSearchPresentationPreservesSearchText(t *testing.T) {
	out := sqlformat.Presentation(SearchOutput{Query: "select worker", Results: []SearchHit{}}).(map[string]any)
	if out["query"] != "select worker" {
		t.Fatalf("search text formatted as SQL: %#v", out)
	}
}

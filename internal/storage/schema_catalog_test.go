package storage

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestGenerateSchemaCatalogUsesOnlyPublicViews(t *testing.T) {
	catalog, err := GenerateSchemaCatalog(filepath.Join(t.TempDir(), "schema.duckdb"))
	if err != nil {
		t.Fatal(err)
	}
	if catalog.Fingerprint == "" || len(catalog.Views) != len(schemaViewMetadata) {
		t.Fatalf("unexpected catalog: %#v", catalog)
	}
	for _, view := range catalog.Views {
		if len(view.Columns) == 0 || !strings.HasPrefix(view.Name, "telemetry_") {
			t.Fatalf("invalid public view: %#v", view)
		}
		if _, ok := schemaViewMetadata[view.Name]; !ok {
			t.Fatalf("unexpected public view: %s", view.Name)
		}
	}
}

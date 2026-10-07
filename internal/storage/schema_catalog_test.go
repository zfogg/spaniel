package storage

import (
	"path/filepath"
	"testing"
)

func TestGenerateSchemaCatalogUsesOnlyPublicViews(t *testing.T) {
	catalog, err := GenerateSchemaCatalog(filepath.Join(t.TempDir(), "schema.duckdb"))
	if err != nil {
		t.Fatal(err)
	}
	if catalog.Fingerprint == "" || len(catalog.Views) != 4 {
		t.Fatalf("unexpected catalog: %#v", catalog)
	}
	for _, view := range catalog.Views {
		if len(view.Columns) == 0 || view.Name[:10] != "telemetry_" {
			t.Fatalf("invalid public view: %#v", view)
		}
	}
}

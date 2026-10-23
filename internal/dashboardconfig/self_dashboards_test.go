package dashboardconfig

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRepositorySelfDashboardsKeepStableIDsAndBindings(t *testing.T) {
	root := filepath.Join("..", "..", "deploy", "spaniel", "dashboards")
	files, err := filepath.Glob(filepath.Join(root, "*.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{"spaniel-self-runtime": false, "spaniel-self-api": false, "spaniel-self-ingestion": false}
	if len(files) != len(want) {
		t.Fatalf("got %d dashboard files, want %d", len(files), len(want))
	}
	types := map[string]bool{}
	for _, file := range files {
		t.Run(filepath.Base(file), func(t *testing.T) {
			data, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			definition, err := Parse(data)
			if err != nil {
				t.Fatal(err)
			}
			seen, ok := want[definition.ID]
			if !ok || seen {
				t.Fatalf("unexpected or duplicate stable dashboard ID %q", definition.ID)
			}
			want[definition.ID] = true
			dashboard, err := definition.Dashboard("must-not-replace-the-file-id")
			if err != nil {
				t.Fatal(err)
			}
			encoded, err := Marshal(dashboard)
			if err != nil {
				t.Fatal(err)
			}
			restored, err := Parse(encoded)
			if err != nil || restored.ID != definition.ID {
				t.Fatalf("stable ID did not survive export/import: %v", err)
			}
			for _, panel := range definition.Panels {
				types[panel.DisplayType] = true
				if !strings.Contains(panel.Query, "$service") || !strings.Contains(panel.Query, "$session_id") {
					t.Errorf("%q must stay scoped to the selected service and active session", panel.Title)
				}
			}
		})
	}
	for _, display := range []string{"bar", "pie", "gauge"} {
		if !types[display] {
			t.Errorf("self-observability pack lost its %s panels", display)
		}
	}
}

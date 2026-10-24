package yamlquery

import (
	"bytes"
	"testing"
)

func TestFormatAndIdempotence(t *testing.T) {
	input := []byte("# retained\nquery: SELECT a,b FROM spans WHERE x=1\ninstance_discovery:\n  query: SELECT a,b FROM spans\nvariables:\n- source: spans.service_name\n- source: SELECT a,b FROM spans\npanels:\n- query: SELECT a,b FROM spans\n  settings:\n    annotation_query: SELECT a,b FROM spans\nannotations:\n  query: SELECT do,not,change\n")
	got, changed, err := Format(input)
	if err != nil || !changed {
		t.Fatalf("%v %v", changed, err)
	}
	for _, keep := range []string{"# retained", "source: spans.service_name", "query: SELECT do,not,change", "annotation_query: |"} {
		if !bytes.Contains(got, []byte(keep)) {
			t.Fatalf("missing %q: %s", keep, got)
		}
	}
	twice, changed, err := Format(got)
	if err != nil || changed || !bytes.Equal(got, twice) {
		t.Fatalf("not idempotent: %v %v", changed, err)
	}
}
func TestUnchangedAndMalformed(t *testing.T) {
	for _, input := range []string{"query: |\n  SELECT\n    1\n", "name: unchanged\n"} {
		got, changed, err := Format([]byte(input))
		if err != nil || changed || string(got) != input {
			t.Fatalf("%q %v %v", got, changed, err)
		}
	}
	for _, input := range []string{"query: [", "query: SELECT 1\n---\nquery: SELECT 2"} {
		if _, _, err := Format([]byte(input)); err == nil {
			t.Fatal("accepted malformed document")
		}
	}
}

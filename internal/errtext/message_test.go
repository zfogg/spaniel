package errtext

import (
	"strings"
	"testing"
)

func TestMessageCollapsesInvalidationAndPreservesCause(t *testing.T) {
	message := strings.Repeat("Failed: "+invalidated+". Original error: \"", 10000) + "Could not find node in column segment tree" + strings.Repeat("\"", 10000)
	got := Message(message)
	if strings.Count(got, "Original cause:") != 1 || !strings.Contains(got, "Could not find node") || len(got) > Limit {
		t.Fatal(got)
	}
	if Message("permission denied") != "permission denied" {
		t.Fatal("ordinary error changed")
	}
	if len(Message(strings.Repeat("a", 10000))) > Limit+32 {
		t.Fatal("unbounded error")
	}
}

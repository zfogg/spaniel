package ingestion

import (
	"encoding/json"
	"fmt"
	"github.com/zfogg/spaniel/internal/ingestion/detectors"
	"strings"
	"testing"

	"github.com/zfogg/spaniel/internal/ingestion/linter"
	"github.com/zfogg/spaniel/internal/storage"
)

func TestN1VersionedFingerprintOmittedTextAndDatabaseScope(t *testing.T) {
	var spans []*storage.Span
	for group := range 3 {
		for i := range 3 {
			attrs := map[string]any{"db.system.name": "postgresql", "db.operation.name": "SELECT", "gotel.db.query.text_reason": "policy", "gotel.db.query.fingerprint": "sequel:v1:postgresql:" + strings.Repeat("a", 64), "db.namespace": fmt.Sprint(group % 2)}
			service := "api"
			if group == 2 {
				service = "worker"
			}
			raw, _ := json.Marshal(attrs)
			spans = append(spans, &storage.Span{TraceID: "trace", SpanID: fmt.Sprintf("%d-%d", group, i), ParentSpanID: "loop", ServiceName: service, Attributes: string(raw)})
		}
	}
	spans = append(spans, spans[0]) // retransmission must not inflate a loop
	issues := (&detectors.N1Detector{}).Analyze("trace", "session", spans, 0)
	if len(issues) != 3 {
		t.Fatalf("database/service scopes combined: %+v", issues)
	}
	ids := map[string]bool{}
	for _, issue := range issues {
		if issue.Count != 3 || !strings.HasPrefix(issue.Fingerprint, "sequel:v1:postgresql:") {
			t.Fatalf("wrong occurrence/shape: %+v", issue)
		}
		if ids[issue.ID] {
			t.Fatal("scope-specific issues share ID")
		}
		ids[issue.ID] = true
	}
}

func TestStorageQueryNamesSharedRegistryCanonicalAndLegacyNames(t *testing.T) {
	for _, name := range []string{"storage.postgres.FindAccount", "app.storage.FindAccount"} {
		first := queryNameSpan("a", "trace", name, "SELECT id FROM accounts WHERE id=1")
		second := queryNameSpan("b", "trace", name, "SELECT id FROM accounts WHERE id=99")
		if got := linter.QueryNameLints([]*storage.Span{first, second}, 1); len(got) != 0 {
			t.Fatal("literal values created a false query-name collision")
		}
		third := queryNameSpan("c", "trace", name, "SELECT balance FROM escrows")
		got := linter.QueryNameLints([]*storage.Span{first, second, third, first}, 1)
		if len(got) != 3 {
			t.Fatalf("shared registry must flag each distinct occurrence once: %+v", got)
		}
		other := queryNameSpan("d", "trace", name, "SELECT balance FROM escrows")
		other.ServiceName = "other-service"
		if got := linter.QueryNameLints([]*storage.Span{first, other}, 1); len(got) != 0 {
			t.Fatal("different service databases combined")
		}
	}
}

func TestStorageQueryNamesDistinctFixedLiteralDefinitionsAreAllowed(t *testing.T) {
	spans := []*storage.Span{
		queryNameSpan("alert", "trace", "storage.postgres.GetAlertTime", "SELECT updated_at FROM checkpoints WHERE key='alert'"),
		queryNameSpan("fill", "trace", "storage.postgres.GetLastNotifiedFillTime", "SELECT updated_at FROM checkpoints WHERE key='fill'"),
	}
	if warnings := linter.QueryNameLints(spans, 1); len(warnings) != 0 {
		t.Fatal("privacy fingerprints cannot prove distinct source-owned names duplicate static definitions")
	}
}

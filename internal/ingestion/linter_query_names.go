package ingestion

import (
	"context"
	"strings"

	"github.com/zfogg/spaniel/internal/storage"
	"github.com/zfogg/spaniel/internal/telemetry"
)

const storageQueryNameRule = "db.storage_name_collision"

func persistStorageQueryNameLints(traceID string, store *storage.DB, spans []*storage.Span, now int64) {
	if count, err := store.InsertMissingTraceLints(traceID, storageQueryNameRule, storageQueryNameLints(spans, now)); err == nil {
		for i := 0; i < count; i++ {
			telemetry.Catalog().RecordLintWarning(context.Background(), storageQueryNameRule, "warning")
		}
	}
}

// Query names identify SQL templates, not merely their enclosing Go function.
// Compare exact db.query.text values within the same trace, including repeated
// copies of each template, so the first arrival is flagged retroactively too.
func storageQueryNameLints(spans []*storage.Span, now int64) []*storage.LintWarning {
	type key struct{ traceID, name string }
	type group struct {
		spans []*storage.Span
		texts map[string]bool
	}
	groups := map[key]*group{}
	for _, s := range spans {
		if isStatementPreparation(s) {
			continue
		}
		prefix, name, ok := strings.Cut(s.Name, ".storage.")
		if !ok || prefix == "" || name == "" {
			continue
		}
		text := extractAttrString(s.Attributes, "db.query.text")
		if text == "" {
			continue
		}
		k := key{s.TraceID, s.Name}
		g := groups[k]
		if g == nil {
			g = &group{texts: map[string]bool{}}
			groups[k] = g
		}
		g.spans = append(g.spans, s)
		g.texts[text] = true
	}
	var result []*storage.LintWarning
	for k, g := range groups {
		if len(g.texts) < 2 {
			continue
		}
		for _, s := range g.spans {
			result = append(result, &storage.LintWarning{
				SpanID: s.SpanID, TraceID: s.TraceID, SessionID: s.SessionID,
				RuleID: storageQueryNameRule, Severity: "warning", CreatedAt: now,
				Message: "Storage span name " + k.name + " identifies different db.query.text values in this trace; assign a distinct query name to each SQL template",
			})
		}
	}
	return result
}

// Preparation describes a driver operation, not execution of a named query.
// Do not infer it from a query name (PrepareOrders may be a real named query),
// or from db.stored_procedure.name (also valid for procedure executions).
func isStatementPreparation(s *storage.Span) bool {
	for _, key := range []string{"db.operation.name", "db.operation"} {
		if strings.EqualFold(extractAttrString(s.Attributes, key), "PREPARE") {
			return true
		}
	}
	// pgx instrumentation historically reports the underlying SQL verb and
	// marks prepare spans with this boolean, including false on first prepare.
	_, present := extractAttrBool(s.Attributes, "db.operation.already_prepared")
	return present
}

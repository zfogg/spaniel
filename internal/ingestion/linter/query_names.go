package linter

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/zfogg/gotel/go/querynames"
	gotelsql "github.com/zfogg/gotel/go/tracers/sql"
	"github.com/zfogg/spaniel/internal/querydiagnostics"
	"github.com/zfogg/spaniel/internal/storage"
	"github.com/zfogg/spaniel/internal/telemetry"
)

const StorageQueryNameRule = "db.storage_name_collision"

// PersistStorageQueryNameLints records storage query names that identify more
// than one SQL template within the same trace.
func PersistStorageQueryNameLints(traceID string, store *storage.DB, spans []*storage.Span, now int64) {
	if count, err := store.InsertMissingTraceLints(traceID, StorageQueryNameRule, QueryNameLints(spans, now)); err == nil {
		for i := 0; i < count; i++ {
			telemetry.Catalog().RecordLintWarning(context.Background(), StorageQueryNameRule, "warning")
		}
	}
}

func extractAttrString(attrsJSON, key string) string {
	var attrs map[string]any
	if err := json.Unmarshal([]byte(attrsJSON), &attrs); err != nil {
		return ""
	}
	value, _ := attrs[key].(string)
	return value
}

func extractAttrBool(attrsJSON, key string) (bool, bool) {
	var attrs map[string]any
	if err := json.Unmarshal([]byte(attrsJSON), &attrs); err != nil {
		return false, false
	}
	value, ok := attrs[key]
	if !ok {
		return false, false
	}
	boolean, ok := value.(bool)
	return boolean, ok
}

// QueryNameLints uses Gotel's shared identity checker to find a storage name
// assigned to different versioned fingerprints within a trace/database boundary.
// Every occurrence is flagged, including arrivals before the collision was seen.
// SQL text is never retained by the registry or copied into a finding.
func QueryNameLints(spans []*storage.Span, now int64) []*storage.LintWarning {
	type key struct{ traceID, name, identity string }
	type group struct {
		spans     []*storage.Span
		registry  *querynames.Registry
		collision bool
	}
	groups := map[key]*group{}
	for _, s := range spans {
		if IsStatementPreparation(s) {
			continue
		}
		var simple string
		if strings.HasPrefix(s.Name, "storage.") {
			parts := strings.Split(s.Name, ".")
			if len(parts) != 3 {
				continue
			}
			simple = parts[2]
		} else {
			prefix, name, ok := strings.Cut(s.Name, ".storage.")
			if !ok || prefix == "" {
				continue
			}
			simple = name
		}
		if _, err := (gotelsql.Namer{}).Parse(simple); err != nil {
			continue
		}
		attrs := map[string]any{}
		if json.Unmarshal([]byte(s.Attributes), &attrs) != nil {
			continue
		}
		fp := querydiagnostics.Fingerprint(attrs)
		if fp == "" {
			continue
		}
		k := key{s.TraceID, s.Name, querydiagnostics.Identity(attrs, s.ServiceName, "")}
		g := groups[k]
		if g == nil {
			g = &group{registry: querynames.NewRegistry(1)}
			groups[k] = g
		}
		g.spans = append(g.spans, s)
		if g.registry.ObserveFingerprint(simple, fp) != nil {
			g.collision = true
		}
	}
	var result []*storage.LintWarning
	for _, g := range groups {
		if !g.collision {
			continue
		}
		seen := map[string]bool{}
		for _, s := range g.spans {
			if seen[s.SpanID] {
				continue
			}
			seen[s.SpanID] = true
			result = append(result, &storage.LintWarning{SpanID: s.SpanID, TraceID: s.TraceID, SessionID: s.SessionID, RuleID: StorageQueryNameRule, Severity: "warning", CreatedAt: now, Message: "Storage query name identifies different versioned SQL fingerprints in this trace and database; assign a distinct source-owned name to each query definition"})
		}
	}
	return result
}

// Preparation describes a driver operation, not execution of a named query.
// Do not infer it from a query name (PrepareOrders may be a real named query),
// or from db.stored_procedure.name (also valid for procedure executions).
// IsStatementPreparation reports whether a database span describes preparing
// a statement rather than executing the named query.
func IsStatementPreparation(s *storage.Span) bool {
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

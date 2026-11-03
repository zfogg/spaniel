// Package traceassert checks captured telemetry without exporting it or requiring
// a running Spaniel collector. Specs are portable between all instrumented apps.
package traceassert

import (
	"encoding/json"
	"fmt"
	"io"
	"reflect"
	"sort"
	"strings"
)

// Selector matches the conjunction of its predicates. An empty selector matches all spans.
type Selector struct {
	// TraceID restricts matches to one lowercase hexadecimal trace ID; empty is unrestricted.
	TraceID string `json:"trace_id,omitempty"`
	// ParentSpanID matches an exact parent; a pointer to empty string selects roots.
	ParentSpanID *string `json:"parent_span_id,omitempty"`
	// Service matches the exact resource service.name.
	Service string `json:"service,omitempty"`
	// ServicePrefix matches a service-name prefix, useful for worker replicas.
	ServicePrefix string `json:"service_prefix,omitempty"`
	// Name matches an exact operation name.
	Name string `json:"name,omitempty"`
	// NamePrefix matches an operation-name prefix.
	NamePrefix string `json:"name_prefix,omitempty"`
	// Kind matches an OTLP SpanKind integer when non-nil.
	Kind *int `json:"kind,omitempty"`
	// StatusCode matches OTLP status numbering: unset 0, OK 1, error 2.
	StatusCode *int `json:"status_code,omitempty"`
	// Attributes matches exact JSON values, including their scalar types.
	Attributes map[string]any `json:"attributes,omitempty"`
	// HasAttributes requires each key to exist regardless of value.
	HasAttributes []string `json:"has_attributes,omitempty"`
	// AbsentAttributes requires each key to be absent.
	AbsentAttributes []string `json:"absent_attributes,omitempty"`
}

// Count asserts inclusive cardinality bounds; nil Max means no upper bound.
type Count struct {
	// Name is a unique report label; use a static label without secrets.
	Name string `json:"name"`
	// Select identifies the spans counted.
	Select Selector `json:"select"`
	// Min is the inclusive lower bound; zero permits no matches.
	Min int `json:"min"`
	// Max is the inclusive upper bound; nil means unbounded.
	Max *int `json:"max,omitempty"`
}

// Ancestry requires each selected terminal span to have the ordered ancestor path.
type Ancestry struct {
	// Name is the unique static report label.
	Name string `json:"name"`
	// Path lists ancestor-to-descendant selectors; at least two are required.
	Path []Selector `json:"path"`
	// Min is the minimum terminal-span count; nil defaults to one.
	Min *int `json:"min,omitempty"`
	// Direct requires every path stage to be an immediate causal parent.
	Direct bool `json:"direct,omitempty"`
	// AllowLinks permits descendant-to-ancestor links, including cross-trace links.
	AllowLinks bool `json:"allow_links,omitempty"`
}

// DuplicateOperations rejects multiple selected spans with identical group fields.
type DuplicateOperations struct {
	// Name is the unique static report label.
	Name string `json:"name"`
	// Select identifies executions subject to duplicate detection.
	Select Selector `json:"select"`
	// GroupBy lists trace_id, parent_span_id, service_name, name or attributes.<key>; missing fields fail.
	GroupBy []string `json:"group_by"`
}

// Redaction checks all captured attributes, resources, events and links recursively.
type Redaction struct {
	// ForbiddenKeys rejects these keys recursively in attributes, resources, events and links.
	ForbiddenKeys []string `json:"forbidden_keys,omitempty"`
	// ForbiddenValues rejects each nonempty substring recursively, including typed SDK arrays.
	ForbiddenValues []string `json:"forbidden_values,omitempty"`
	// RequireQueryTextReason validates absent-text reasons on SQL analysis spans; lifecycle and non-SQL spans are exempt.
	RequireQueryTextReason bool `json:"require_query_text_reason,omitempty"`
	// ValidateDiagnostics requires and validates catalog diagnostics on SQL analysis spans.
	ValidateDiagnostics bool `json:"validate_diagnostics,omitempty"`
}

// Graph adds whole-capture completeness assertions, independently of path rules.
type Graph struct {
	// RequireResolvedParents fails if a non-root parent is missing from the capture.
	RequireResolvedParents bool `json:"require_resolved_parents,omitempty"`
	// RequireSingleRoot requires exactly one parentless span across the combined capture.
	RequireSingleRoot bool `json:"require_single_root,omitempty"`
	// RequireConnected requires one undirected component using parent and causal-link edges.
	RequireConnected bool `json:"require_connected,omitempty"`
}

// Finding is a normalized lint or detector finding captured by a consumer.
type Finding struct {
	// RuleID identifies the lint rule.
	RuleID string `json:"rule_id"`
	// SpanID identifies the associated span.
	SpanID string `json:"span_id"`
	// TraceID optionally records the associated trace; association rules currently compare span IDs.
	TraceID string `json:"trace_id"`
}

// FindingRule checks associated span IDs for one rule, optionally exactly.
type FindingRule struct {
	// Name is the unique static report label.
	Name string `json:"name"`
	// RuleID selects the captured lint rule.
	RuleID string `json:"rule_id"`
	// SpanIDs lists all required affected span IDs.
	SpanIDs []string `json:"span_ids"`
	// Exact also rejects affected IDs absent from SpanIDs.
	Exact bool `json:"exact"`
}

// DiagnosticRule asserts the exact canonical diagnostic-code multiset on every
// selected span, plus an optional exact omission reason (empty means absent).
type DiagnosticRule struct {
	// Name is the unique static report label.
	Name string `json:"name"`
	// Select identifies every span whose diagnostic result must match.
	Select Selector `json:"select"`
	// Codes is the exact code multiset; an explicit empty slice requires a clean empty array.
	Codes []string `json:"codes"`
	// TextReason optionally requires an exact omission reason; empty string requires absence.
	TextReason *string `json:"text_reason,omitempty"`
	// Min requires at least this many selected spans; nil defaults to one.
	Min *int `json:"min,omitempty"`
	// NormalizedOnly rejects encoded SDK diagnostic strings and requires structured objects.
	NormalizedOnly bool `json:"normalized_only,omitempty"`
}

// Spec is the strict versioned portable assertion format.
type Spec struct {
	// Version must be 1; unsupported schemas fail closed.
	Version int `json:"version"`
	// NoDuplicateSpanIDs rejects repeated trace/span ID pairs, including retransmissions.
	NoDuplicateSpanIDs bool `json:"no_duplicate_span_ids,omitempty"`
	// NoDuplicateOperations checks source-owned operation identity tuples.
	NoDuplicateOperations []DuplicateOperations `json:"no_duplicate_operations,omitempty"`
	// Counts checks inclusive selector cardinalities.
	Counts []Count `json:"counts,omitempty"`
	// Ancestry checks ordered paths for every selected terminal span.
	Ancestry []Ancestry `json:"ancestry,omitempty"`
	// Redaction configures recursive privacy and SQL wire checks.
	Redaction Redaction `json:"redaction,omitempty"`
	// Graph enables whole-capture completeness checks; parent cycles are always rejected.
	Graph Graph `json:"graph,omitempty"`
	// Findings checks associations from a separate findings capture.
	Findings []FindingRule `json:"findings,omitempty"`
	// Diagnostics checks exact per-span codes and omission expectations.
	Diagnostics []DiagnosticRule `json:"diagnostics,omitempty"`
}

// Failure identifies a rule without including potentially sensitive capture values.
type Failure struct {
	// Rule is a static configured label or built-in category; capture values are never copied.
	Rule string `json:"rule"`
	// Code is a stable machine-readable failure identifier.
	Code string `json:"code"`
	// Count is the relevant observed count; zero may be omitted from JSON.
	Count int `json:"count,omitempty"`
}

// Report is deterministic and suitable for machine-readable test evidence.
type Report struct {
	// Version identifies the report schema, currently 1.
	Version int `json:"version"`
	// SpanCount includes all supplied spans, including duplicates.
	SpanCount int `json:"span_count"`
	// Failures is empty on success and sorted deterministically by rule, code and count.
	Failures []Failure `json:"failures"`
}

// OK reports whether every assertion passed.
func (r Report) OK() bool { return len(r.Failures) == 0 }

// DecodeSpec rejects unknown fields, trailing data, unsupported versions and invalid bounds.
func DecodeSpec(r io.Reader) (Spec, error) {
	var spec Spec
	d := json.NewDecoder(io.LimitReader(r, 1<<20))
	d.DisallowUnknownFields()
	if d.Decode(&spec) != nil {
		return spec, fmt.Errorf("invalid assertion specification")
	}
	if d.Decode(new(any)) != io.EOF {
		return spec, fmt.Errorf("trailing specification data")
	}
	return spec, spec.validate()
}

func (s Spec) validate() error {
	if s.Version != 1 {
		return fmt.Errorf("unsupported assertion version")
	}
	if len(s.Counts)+len(s.Ancestry)+len(s.NoDuplicateOperations)+len(s.Findings)+len(s.Diagnostics) == 0 && !s.NoDuplicateSpanIDs && !s.Graph.RequireResolvedParents && !s.Graph.RequireSingleRoot && !s.Graph.RequireConnected && !s.Redaction.RequireQueryTextReason && !s.Redaction.ValidateDiagnostics && len(s.Redaction.ForbiddenKeys)+len(s.Redaction.ForbiddenValues) == 0 {
		return fmt.Errorf("empty assertion specification")
	}
	names := map[string]bool{}
	name := func(n string) bool {
		if n == "" || names[n] {
			return false
		}
		names[n] = true
		return true
	}
	for _, c := range s.Counts {
		if !name(c.Name) || c.Min < 0 || c.Max != nil && *c.Max < c.Min {
			return fmt.Errorf("invalid count rule")
		}
	}
	for _, a := range s.Ancestry {
		if !name(a.Name) || len(a.Path) < 2 || a.Min != nil && *a.Min < 1 {
			return fmt.Errorf("invalid ancestry rule")
		}
	}
	for _, d := range s.NoDuplicateOperations {
		if !name(d.Name) || len(d.GroupBy) == 0 {
			return fmt.Errorf("invalid duplicate rule")
		}
		for _, f := range d.GroupBy {
			if f != "trace_id" && f != "parent_span_id" && f != "service_name" && f != "name" && !(strings.HasPrefix(f, "attributes.") && len(f) > 11) {
				return fmt.Errorf("invalid duplicate group field")
			}
		}
	}
	for _, v := range s.Redaction.ForbiddenValues {
		if v == "" {
			return fmt.Errorf("empty forbidden value")
		}
	}
	for _, f := range s.Findings {
		if !name(f.Name) || f.RuleID == "" {
			return fmt.Errorf("invalid finding rule")
		}
	}
	for _, d := range s.Diagnostics {
		if !name(d.Name) || d.Codes == nil || d.Min != nil && *d.Min < 1 {
			return fmt.Errorf("invalid diagnostic rule")
		}
	}
	return nil
}

func (p Selector) matches(s Span) bool {
	if p.ServicePrefix != "" && !strings.HasPrefix(s.ServiceName, p.ServicePrefix) {
		return false
	}
	if p.TraceID != "" && p.TraceID != s.TraceID || p.Service != "" && p.Service != s.ServiceName || p.Name != "" && p.Name != s.Name || p.NamePrefix != "" && !strings.HasPrefix(s.Name, p.NamePrefix) || p.Kind != nil && *p.Kind != s.Kind {
		return false
	}
	if p.ParentSpanID != nil && *p.ParentSpanID != s.ParentSpanID || p.StatusCode != nil && *p.StatusCode != s.StatusCode {
		return false
	}
	for k, v := range p.Attributes {
		actual, ok := s.Attributes[k]
		if !ok {
			return false
		}
		a, _ := json.Marshal(actual)
		b, _ := json.Marshal(v)
		if string(a) != string(b) {
			return false
		}
	}
	for _, k := range p.HasAttributes {
		if _, ok := s.Attributes[k]; !ok {
			return false
		}
	}
	for _, k := range p.AbsentAttributes {
		if _, ok := s.Attributes[k]; ok {
			return false
		}
	}
	return true
}

// Check validates graph structure and applies all rules. It never includes capture
// values in failures, including on invalid input. Duplicate captures are retained
// so retransmissions can be detected rather than silently hidden.
func Check(spans []Span, spec Spec) Report { return CheckWithFindings(spans, nil, spec) }

// CheckWithFindings additionally checks consumer-captured lint associations.
func CheckWithFindings(spans []Span, findings []Finding, spec Spec) Report {
	r := Report{Version: 1, SpanCount: len(spans), Failures: []Failure{}}
	fail := func(rule, code string, n int) { r.Failures = append(r.Failures, Failure{rule, code, n}) }
	if spec.validate() != nil {
		fail("spec", "invalid_spec", 0)
		return r
	}
	if len(spans) > 100000 {
		fail("graph", "capture_span_limit", len(spans))
		return r
	}
	index := map[string]int{}
	for i, s := range spans {
		if !validID(s.TraceID, 32) || !validID(s.SpanID, 16) || s.ParentSpanID != "" && !validID(s.ParentSpanID, 16) {
			fail("graph", "invalid_id", 1)
			continue
		}
		if _, ok := index[s.key()]; ok {
			if spec.NoDuplicateSpanIDs {
				fail("graph", "duplicate_span_id", 1)
			}
		} else {
			index[s.key()] = i
		}
	}
	parents := func(i int, links bool) []int {
		s := spans[i]
		out := []int{}
		if p, ok := index[s.TraceID+"/"+s.ParentSpanID]; s.ParentSpanID != "" && ok {
			out = append(out, p)
		}
		if links {
			for _, l := range s.Links {
				if p, ok := index[l.TraceID+"/"+l.SpanID]; ok {
					out = append(out, p)
				}
			}
		}
		return out
	}
	// Parent cycles are invalid even if no ancestry rule traverses them.
	colors := make([]uint8, len(spans))
	var cycle func(int) bool
	cycle = func(i int) bool {
		if colors[i] == 1 {
			return true
		}
		if colors[i] == 2 {
			return false
		}
		colors[i] = 1
		for _, p := range parents(i, false) {
			if cycle(p) {
				return true
			}
		}
		colors[i] = 2
		return false
	}
	for i := range spans {
		if cycle(i) {
			fail("graph", "parent_cycle", 1)
			break
		}
	}
	if spec.Graph.RequireSingleRoot || spec.Graph.RequireResolvedParents || spec.Graph.RequireConnected {
		roots, unresolved := 0, 0
		for _, s := range spans {
			if s.ParentSpanID == "" {
				roots++
			} else if _, ok := index[s.TraceID+"/"+s.ParentSpanID]; !ok {
				unresolved++
			}
		}
		if spec.Graph.RequireSingleRoot && roots != 1 {
			fail("graph", "root_count", roots)
		}
		if spec.Graph.RequireResolvedParents && unresolved > 0 {
			fail("graph", "unresolved_parent", unresolved)
		}
		if spec.Graph.RequireConnected && len(spans) > 0 {
			adj := make([][]int, len(spans))
			for i := range spans {
				for _, p := range parents(i, true) {
					adj[i] = append(adj[i], p)
					adj[p] = append(adj[p], i)
				}
			}
			seen := map[int]bool{0: true}
			todo := []int{0}
			for len(todo) > 0 {
				i := todo[len(todo)-1]
				todo = todo[:len(todo)-1]
				for _, p := range adj[i] {
					if !seen[p] {
						seen[p] = true
						todo = append(todo, p)
					}
				}
			}
			if len(seen) != len(spans) {
				fail("graph", "disconnected", len(spans)-len(seen))
			}
		}
	}
	for _, c := range spec.Counts {
		n := 0
		for _, s := range spans {
			if c.Select.matches(s) {
				n++
			}
		}
		if n < c.Min || c.Max != nil && n > *c.Max {
			fail(c.Name, "count_out_of_bounds", n)
		}
	}
	for _, d := range spec.NoDuplicateOperations {
		seen := map[string]bool{}
		duplicates, missing := 0, 0
		for _, s := range spans {
			if !d.Select.matches(s) {
				continue
			}
			values := []any{}
			for _, f := range d.GroupBy {
				var v any
				var ok = true
				switch f {
				case "trace_id":
					v = s.TraceID
				case "parent_span_id":
					v = s.ParentSpanID
				case "service_name":
					v = s.ServiceName
				case "name":
					v = s.Name
				default:
					v, ok = s.Attributes[strings.TrimPrefix(f, "attributes.")]
				}
				if !ok {
					missing++
				}
				values = append(values, v)
			}
			b, _ := json.Marshal(values)
			if seen[string(b)] {
				duplicates++
			}
			seen[string(b)] = true
		}
		if missing > 0 {
			fail(d.Name, "missing_group_field", missing)
		}
		if duplicates > 0 {
			fail(d.Name, "duplicate_operation", duplicates)
		}
	}
	for _, a := range spec.Ancestry {
		matched, bad := 0, 0
		terminals := map[string]bool{}
		work := 0
		exhausted := false
		// Search states include path stage: one ancestor cannot satisfy two
		// stages, and cycles/diamonds cannot inflate the terminal span count.
		type state struct{ i, stage int }
		for i, s := range spans {
			if !a.Path[len(a.Path)-1].matches(s) {
				continue
			}
			if terminals[s.key()] {
				continue
			}
			terminals[s.key()] = true
			seen := map[state]bool{}
			var search func(int, int) bool
			search = func(child, stage int) bool {
				work++
				if work > 1000000 {
					exhausted = true
					return false
				}
				if stage < 0 {
					return true
				}
				st := state{child, stage}
				if seen[st] {
					return false
				}
				seen[st] = true
				for _, p := range parents(child, a.AllowLinks) {
					if a.Path[stage].matches(spans[p]) && search(p, stage-1) {
						return true
					}
					if !a.Direct && search(p, stage) {
						return true
					}
				}
				return false
			}
			if search(i, len(a.Path)-2) {
				matched++
			} else {
				bad++
			}
			if exhausted {
				break
			}
		}
		min := 1
		if a.Min != nil {
			min = *a.Min
		}
		if matched < min {
			fail(a.Name, "missing_ancestry", matched)
		}
		if bad > 0 {
			fail(a.Name, "broken_ancestry", bad)
		}
		if exhausted {
			fail(a.Name, "ancestry_work_limit", work)
		}
	}
	for _, s := range spans {
		checkRedaction(s, spec.Redaction, fail)
	}
	for _, f := range spec.Findings {
		expected := map[string]bool{}
		actual := map[string]bool{}
		for _, id := range f.SpanIDs {
			expected[id] = true
		}
		for _, row := range findings {
			if row.RuleID == f.RuleID {
				actual[row.SpanID] = true
			}
		}
		missing, unexpected := 0, 0
		for id := range expected {
			if !actual[id] {
				missing++
			}
		}
		for id := range actual {
			if !expected[id] {
				unexpected++
			}
		}
		if missing > 0 {
			fail(f.Name, "missing_finding", missing)
		}
		if f.Exact && unexpected > 0 {
			fail(f.Name, "unexpected_finding", unexpected)
		}
	}
	for _, rule := range spec.Diagnostics {
		selected := 0
		for _, s := range spans {
			if !rule.Select.matches(s) {
				continue
			}
			selected++
			if rule.NormalizedOnly && !normalizedDiagnostics(s.Attributes) {
				fail(rule.Name, "diagnostics_not_normalized", 1)
			}
			codes, valid := diagnosticCodes(s.Attributes)
			if !valid {
				fail(rule.Name, "invalid_diagnostic_wire_format", 1)
			} else {
				expected := append([]string{}, rule.Codes...)
				sort.Strings(codes)
				sort.Strings(expected)
				a, _ := json.Marshal(codes)
				b, _ := json.Marshal(expected)
				if string(a) != string(b) {
					fail(rule.Name, "unexpected_diagnostic_codes", len(codes))
				}
			}
			if rule.TextReason != nil {
				actual, exists := s.Attributes["gotel.db.query.text_reason"]
				if *rule.TextReason == "" {
					if exists {
						fail(rule.Name, "unexpected_text_reason", 1)
					}
				} else if actual != *rule.TextReason {
					fail(rule.Name, "unexpected_text_reason", 1)
				}
			}
		}
		min := 1
		if rule.Min != nil {
			min = *rule.Min
		}
		if selected < min {
			fail(rule.Name, "no_matching_spans", selected)
		}
	}
	sort.SliceStable(r.Failures, func(i, j int) bool {
		a, b := r.Failures[i], r.Failures[j]
		if a.Rule != b.Rule {
			return a.Rule < b.Rule
		}
		if a.Code != b.Code {
			return a.Code < b.Code
		}
		return a.Count < b.Count
	})
	return r
}

func checkRedaction(s Span, red Redaction, fail func(string, string, int)) {
	forbiddenKeys := map[string]bool{}
	for _, k := range red.ForbiddenKeys {
		forbiddenKeys[k] = true
	}
	var walk func(any)
	depth := 0
	depthFailed := false
	walk = func(v any) {
		depth++
		defer func() { depth-- }()
		if depth > 128 {
			if !depthFailed {
				fail("redaction", "attribute_depth_limit", 1)
				depthFailed = true
			}
			return
		}
		switch v := v.(type) {
		case map[string]any:
			for k, x := range v {
				if forbiddenKeys[k] {
					fail("redaction", "forbidden_key", 1)
				}
				walk(x)
			}
		case []any:
			for _, x := range v {
				walk(x)
			}
		case string:
			for _, secret := range red.ForbiddenValues {
				if strings.Contains(v, secret) {
					fail("redaction", "forbidden_value", 1)
				}
			}
		default:
			// SDK callers supply typed slices (for example []string), not
			// necessarily the []any produced by decoding JSON captures.
			rv := reflect.ValueOf(v)
			if rv.IsValid() && (rv.Kind() == reflect.Slice || rv.Kind() == reflect.Array) {
				for i := 0; i < rv.Len(); i++ {
					walk(rv.Index(i).Interface())
				}
			}
		}
	}
	walk(s.Attributes)
	walk(s.Name)
	walk(s.StatusMessage)
	walk(s.Resource)
	walk(s.Events)
	for _, l := range s.Links {
		walk(l.Attributes)
	}
	if red.RequireQueryTextReason {
		text, _ := s.Attributes["db.query.text"].(string)
		legacy, _ := s.Attributes["db.statement"].(string)
		reason, has := s.Attributes["gotel.db.query.text_reason"]
		if text != "" || legacy != "" {
			if has {
				fail("redaction", "unexpected_text_reason", 1)
			}
		} else if sqlAnalysisSpan(s.Attributes) {
			switch reason {
			case "policy", "malformed", "analysis_limit", "analysis_error", "empty", "cancelled":
			default:
				fail("redaction", "missing_or_invalid_text_reason", 1)
			}
		}
	}
	if red.ValidateDiagnostics {
		if value, ok := s.Attributes["db.query.diagnostics"]; ok {
			if !validDiagnostics(s.Attributes, value) {
				fail("diagnostics", "invalid_diagnostic_wire_format", 1)
			}
		} else if sqlAnalysisSpan(s.Attributes) {
			fail("diagnostics", "missing_diagnostics", 1)
		}
	}
}

func sqlAnalysisSpan(attrs map[string]any) bool {
	system, _ := attrs["db.system.name"].(string)
	if system == "" {
		system, _ = attrs["db.system"].(string)
	}
	switch system {
	case "postgresql", "postgres", "sqlite", "sqlite3", "mysql", "mariadb", "duckdb", "clickhouse", "other_sql", "sql", "microsoft.sql_server", "mssql", "oracle", "db2", "h2", "hsqldb", "snowflake", "trino", "presto", "cockroachdb":
	default:
		return false
	}
	op, _ := attrs["db.operation.name"].(string)
	if op == "" {
		op, _ = attrs["db.operation"].(string)
	}
	switch strings.ToUpper(op) {
	case "BEGIN", "COMMIT", "ROLLBACK", "SAVEPOINT", "RELEASE", "ACQUIRE", "CONNECT", "CLOSE", "POOL.ACQUIRE":
		return false
	}
	for _, k := range []string{"gotel.db.query.original_length", "gotel.db.query.analysis_status", "gotel.db.query.fingerprint", "gotel.db.query.text_reason", "db.query.diagnostics"} {
		if _, ok := attrs[k]; ok {
			return true
		}
	}
	return false
}

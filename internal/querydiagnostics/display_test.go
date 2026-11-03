package querydiagnostics

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"

	sequel "github.com/zfogg/sequel/go"
)

func producerDiagnostic(query string, offset, end int, code string) sequel.Diagnostic {
	var d sequel.Diagnostic
	for _, candidate := range sequel.DiagnosticCatalog() {
		if candidate.Code == code {
			d = candidate
			break
		}
	}
	position := func(at int) (int, int) {
		line, column := 1, 1
		for _, r := range query[:at] {
			switch r {
			case '\n':
				line++
				column = 1
			case '\r':
			default:
				column++
			}
		}
		return line, column
	}
	d.Offset, d.EndOffset = offset, end
	d.Line, d.Column = position(offset)
	d.EndLine, d.EndColumn = position(end)
	return d
}

func producerAttributes(query string, formatted bool, diagnostics []sequel.Diagnostic) (string, map[string]any) {
	a := sequel.Analyze(query, sequel.AnalysisOptions{Dialect: sequel.DialectPostgreSQL, Format: true})
	text, display := a.Text, "sanitized"
	if formatted {
		text, display = a.Formatted, "formatted"
	}
	mapping, _ := json.Marshal(SourceMapEnvelope{SchemaVersion: 1, OriginalToSanitized: a.OriginalToSanitized, SanitizedToFormatted: a.SanitizedToFormatted, Display: display})
	return text, map[string]any{Attribute: diagnostics, "gotel.db.query.source_map": string(mapping), "gotel.db.query.original_length": len(query), "gotel.db.query.diagnostic_schema_version": 1}
}

func TestDisplayMapsMultipleDiagnosticsThroughSafeFormattedSQL(t *testing.T) {
	query := "SELECT 'private-long-value', \"😀é\"\r\nFROM things WHERE id IN (1) OR id IN (2)"
	var diagnostics []sequel.Diagnostic
	for offset := strings.Index(query, ")"); offset >= 0; {
		diagnostics = append(diagnostics, producerDiagnostic(query, offset, offset+1, "unmatched_close"))
		next := strings.Index(query[offset+1:], ")")
		if next < 0 {
			break
		}
		offset += next + 1
	}
	for _, formatted := range []bool{false, true} {
		text, attrs := producerAttributes(query, formatted, diagnostics)
		out, err := Display(context.Background(), text, sequel.DialectPostgreSQL, false, attrs)
		if err != nil {
			t.Fatal(err)
		}
		if len(out.Diagnostics) != 2 || strings.Contains(out.SQL, "private-long-value") {
			t.Fatalf("safe multi-diagnostic display: %+v", out)
		}
		for _, d := range out.Diagnostics {
			if d.DisplayStart == nil || d.DisplayEnd == nil {
				t.Fatalf("lost mapped range: %+v", d)
			}
			start, err1 := sequel.UTF16ToUTF8(out.SQL, *d.DisplayStart)
			end, err2 := sequel.UTF16ToUTF8(out.SQL, *d.DisplayEnd)
			if err1 != nil || err2 != nil || out.SQL[start:end] != ")" || d.MappingStatus != "exact" {
				t.Fatalf("wrong transformed range: %+v in %q", d, out.SQL)
			}
			if *d.DisplayStart == diagnostics[0].Offset {
				t.Fatal("applied original byte offset directly to transformed UTF-16 text")
			}
		}
	}
}

func TestDisplayReplacementDeletionAndMissingMap(t *testing.T) {
	query := "SELECT 'private-long-value', \"😀\" /* private-comment */ FROM things"
	start := strings.Index(query, "private-long-value")
	d := producerDiagnostic(query, start, start+len("private-long-value"), "unterminated_string")
	text, attrs := producerAttributes(query, true, []sequel.Diagnostic{d})
	out, err := Display(context.Background(), text, sequel.DialectPostgreSQL, false, attrs)
	if err != nil {
		t.Fatal(err)
	}
	got := out.Diagnostics[0]
	if got.MappingStatus != "approximate" || got.DisplayStart == nil {
		t.Fatalf("replacement wasn't explicit: %+v", got)
	}
	s, _ := sequel.UTF16ToUTF8(out.SQL, *got.DisplayStart)
	e, _ := sequel.UTF16ToUTF8(out.SQL, *got.DisplayEnd)
	if out.SQL[s:e] != "?" {
		t.Fatalf("replacement mapped to %q", out.SQL[s:e])
	}
	start = strings.Index(query, "/*")
	d = producerDiagnostic(query, start, start+len("/* private-comment */"), "unterminated_comment")
	text, attrs = producerAttributes(query, true, []sequel.Diagnostic{d})
	out, err = Display(context.Background(), text, sequel.DialectPostgreSQL, false, attrs)
	if err != nil {
		t.Fatal(err)
	}
	got = out.Diagnostics[0]
	if got.MappingStatus != "approximate" || got.DisplayStart == nil || *got.DisplayStart != *got.DisplayEnd {
		t.Fatalf("deleted source should map to an approximate anchor: %+v", got)
	}
	for _, mapping := range []string{"", `{"schema_version":9}`, `{"schema_version":1,"display":"sanitized","original_to_sanitized":{"from_length":1,"to_length":2,"segments":[]}}`} {
		attrs["gotel.db.query.source_map"] = mapping
		out, err = Display(context.Background(), text, sequel.DialectPostgreSQL, false, attrs)
		if err != nil {
			t.Fatal(err)
		}
		if out.Diagnostics[0].DisplayStart != nil || out.Diagnostics[0].MappingStatus != "unavailable" {
			t.Fatal("applied unmapped original offsets")
		}
	}
}

func TestDisplayOriginalEditorUnicodeCRLFAndAbsentMalformedText(t *testing.T) {
	query := "SELECT \"😀é\",\r\n(1"
	out, err := Display(context.Background(), query, sequel.DialectPostgreSQL, true, nil)
	if err != nil {
		t.Fatal(err)
	}
	if out.SQL != query || len(out.Diagnostics) != 1 {
		t.Fatalf("editor changed current text: %+v", out)
	}
	d := out.Diagnostics[0]
	if d.DisplayStart == nil {
		t.Fatal("editor underline missing")
	}
	want, _ := sequel.UTF8ToUTF16(query, strings.Index(query, "("))
	if *d.DisplayStart != want || d.Line != 2 || d.Column != 1 {
		t.Fatalf("Unicode/CRLF: %+v", d)
	}
	// Telemetry has no safe source for malformed input. Its secret must never
	// become a fallback display, even though its original ranges are retained.
	secret := "SELECT 'private-value"
	diagnostic := sequel.Diagnose(sequel.DialectPostgreSQL, secret)
	attrs := map[string]any{Attribute: []sequel.Diagnostic{diagnostic}, "gotel.db.query.original_length": len(secret), "gotel.db.query.text_reason": "malformed"}
	out, err = Display(context.Background(), "", sequel.DialectPostgreSQL, false, attrs)
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(out)
	if out.SQL != "" || len(out.Diagnostics) != 1 || out.Diagnostics[0].DisplayStart != nil || strings.Contains(string(encoded), "private-value") {
		t.Fatal("omitted malformed source leaked or acquired an underline")
	}
	out, err = Display(context.Background(), secret, sequel.DialectPostgreSQL, false, nil)
	if err != nil || out.SQL != "" {
		t.Fatal("failed telemetry analysis returned raw fallback")
	}
	// An invalid scalar boundary must not be converted into a browser offset.
	if _, err := sequel.UTF8ToUTF16("😀", 1); err == nil {
		t.Fatal("split scalar accepted")
	}
	if utf8.RuneCountInString("é") != 2 {
		t.Fatal("combining-mark fixture changed")
	}
}

func TestRejectedUnknownDiagnosticsCannotBecomeAClaimOfCleanAnalysis(t *testing.T) {
	d := sequel.Diagnose(sequel.DialectPostgreSQL, "SELECT '")
	d.Code = "future_unknown_code"
	attrs := map[string]any{"db.system.name": "postgresql", Attribute: []sequel.Diagnostic{d}}
	Normalize(attrs)
	if attrs["spaniel.db.query.diagnostics_status"] != "invalid" || ValidateAttribute(attrs) {
		t.Fatal("unavailable unknown diagnostic silently became validated []")
	}
}

func TestCatalogValidationVersionsAndOversizeOriginalLength(t *testing.T) {
	var limit sequel.Diagnostic
	for _, d := range sequel.DiagnosticCatalog() {
		if d.Code == "input_limit" {
			limit = d
		}
	}
	limit.Offset, limit.EndOffset, limit.Line, limit.Column, limit.EndLine, limit.EndColumn = 0, 0, 1, 1, 1, 1
	attrs := map[string]any{Attribute: []sequel.Diagnostic{limit}, "db.system.name": "postgresql", "gotel.db.query.original_length": int64(sequel.MaxInputBytes) + 1, "gotel.db.query.diagnostic_schema_version": 1}
	if !ValidateAttribute(attrs) {
		t.Fatal("legitimate input-limit metadata rejected without any large allocation")
	}
	Normalize(attrs)
	if len(attrs[Attribute].([]sequel.Diagnostic)) != 1 {
		t.Fatal("input-limit explanation dropped")
	}
	for _, change := range []map[string]any{{"gotel.db.query.diagnostic_schema_version": 2}, {"gotel.db.query.original_length": -1}, {"gotel.db.query.original_length": 1.5}} {
		copy := map[string]any{}
		for k, v := range attrs {
			copy[k] = v
		}
		for k, v := range change {
			copy[k] = v
		}
		if ValidateAttribute(copy) {
			t.Fatal("invalid version/length accepted")
		}
	}
}

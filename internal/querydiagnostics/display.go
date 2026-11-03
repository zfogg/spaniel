package querydiagnostics

import (
	"bytes"
	"context"
	"encoding/json"
	"io"

	sequel "github.com/zfogg/sequel/go"
)

// SourceMapEnvelope is Gotel's bounded, literal-free provenance for query text.
type SourceMapEnvelope struct {
	// SchemaVersion must be 1; unknown envelopes never authorize underlines.
	SchemaVersion int `json:"schema_version"`
	// OriginalToSanitized maps original UTF-8 bytes to sanitized UTF-8 bytes.
	OriginalToSanitized sequel.SourceMap `json:"original_to_sanitized"`
	// SanitizedToFormatted maps sanitized bytes to the producer's layout.
	SanitizedToFormatted sequel.SourceMap `json:"sanitized_to_formatted"`
	// Display selects which producer document was transported: sanitized or formatted.
	Display string `json:"display"`
}

// DisplayDiagnostic contains canonical prose, original human-readable locations
// and optional global UTF-16 coordinates into DisplayAnalysis.SQL only.
type DisplayDiagnostic struct {
	// Code identifies a validated Sequel catalog entry.
	Code string `json:"code"`
	// Message is trusted catalog prose, never incoming arbitrary text.
	Message string `json:"message"`
	// Explanation describes the problem without SQL or bind values.
	Explanation string `json:"explanation"`
	// Hint is the catalog's suggested corrective action.
	Hint string `json:"hint"`
	// Line is the one-based original line, treating CRLF as one newline.
	Line int `json:"line"`
	// Column is the one-based original Unicode scalar column.
	Column int `json:"column"`
	// EndLine is the original exclusive range end's one-based line.
	EndLine int `json:"end_line"`
	// EndColumn is the exclusive end's one-based Unicode scalar column.
	EndColumn int `json:"end_column"`
	// DisplayStart is a global UTF-16 offset into this result's SQL, or nil without provenance.
	DisplayStart *int `json:"display_start,omitempty"`
	// DisplayEnd is the exclusive global UTF-16 end; an equal start/end is an anchor.
	DisplayEnd *int `json:"display_end,omitempty"`
	// MappingStatus is exact, approximate (replacement/deletion), or unavailable.
	MappingStatus string `json:"mapping_status"`
}

// DisplayAnalysis is safe for rendering. Telemetry never returns failed-analysis
// raw input. Original editor input belongs to its user and may remain visible.
type DisplayAnalysis struct {
	// SQL is safe transformed telemetry text or the exact user-owned editor document.
	SQL string `json:"sql"`
	// Status describes this display analysis; absent input is explicitly not_analyzed.
	Status string `json:"status"`
	// Reason is a bounded omission reason; it never includes input text.
	Reason string `json:"reason"`
	// Diagnostics contains independently mapped canonical entries; empty means no displayed findings.
	Diagnostics []DisplayDiagnostic `json:"diagnostics"`
}

// Display maps producer diagnostic ranges through provenance and the fresh safe
// display transformation. A missing/invalid map leaves locations explanatory;
// it never treats original offsets as positions in transformed text.
func Display(ctx context.Context, query string, dialect sequel.Dialect, original bool, attrs map[string]any) (DisplayAnalysis, error) {
	a := sequel.Analyze(query, sequel.AnalysisOptions{Dialect: dialect, Format: true, OmitBindDescriptors: true, MaxInputBytes: 1 << 20, MaxOutputBytes: 4 << 20, MaxMemoryBytes: 32 << 20, MaxWorkUnits: 32 << 20, Context: ctx})
	out := DisplayAnalysis{Status: a.Status, Reason: a.OmissionReason, Diagnostics: []DisplayDiagnostic{}}
	var diagnostics []sequel.Diagnostic
	var maps []sequel.SourceMap
	if original {
		// Editor diagnostics refer to the exact document currently owned by
		// its user; keep this document stable while the user is typing.
		out.SQL = query
		diagnostics = a.Diagnostics
	} else {
		out.SQL = a.Formatted
		if out.SQL == "" {
			out.SQL = a.Text
		}
		if a.Status != "ok" {
			out.SQL = ""
		}
		if query == "" {
			out.Status = "not_analyzed"
			out.Reason = stringAttr(attrs, "gotel.db.query.text_reason")
			if out.Reason == "" {
				out.Reason = "empty"
			}
		}
		if _, present := attrs[Attribute]; present {
			var err error
			diagnostics, err = DecodeAttribute(attrs)
			if err != nil {
				return out, err
			}
			if stringAttr(attrs, "spaniel.db.query.diagnostics_source") == "display" {
				maps = []sequel.SourceMap{a.OriginalToSanitized, a.SanitizedToFormatted}
			} else if envelope, ok := decodeSourceMap(attrs, query); ok {
				maps = append(maps, envelope.OriginalToSanitized)
				if envelope.Display == "formatted" {
					maps = append(maps, envelope.SanitizedToFormatted)
				}
				maps = append(maps, a.OriginalToSanitized, a.SanitizedToFormatted)
			}
		} else {
			diagnostics = a.Diagnostics
			maps = []sequel.SourceMap{a.OriginalToSanitized, a.SanitizedToFormatted}
		}
	}
	for _, d := range diagnostics {
		item := DisplayDiagnostic{Code: d.Code, Message: d.Message, Explanation: d.Explanation, Hint: d.Hint, Line: d.Line, Column: d.Column, EndLine: d.EndLine, EndColumn: d.EndColumn, MappingStatus: "unavailable"}
		start, end, exact := d.Offset, d.EndOffset, true
		mapped := original || len(maps) > 0
		if !original {
			for _, m := range maps {
				r, err := m.MapRange(start, end, false)
				if err != nil {
					mapped = false
					break
				}
				start, end = r.Start, r.End
				exact = exact && r.Exact
			}
		}
		if mapped && out.SQL != "" {
			s, err1 := sequel.UTF8ToUTF16(out.SQL, start)
			e, err2 := sequel.UTF8ToUTF16(out.SQL, end)
			if err1 == nil && err2 == nil && e >= s {
				item.DisplayStart = &s
				item.DisplayEnd = &e
				item.MappingStatus = "exact"
				if !exact {
					item.MappingStatus = "approximate"
				}
			}
		}
		out.Diagnostics = append(out.Diagnostics, item)
	}
	return out, nil
}

func decodeSourceMap(attrs map[string]any, query string) (SourceMapEnvelope, bool) {
	var out SourceMapEnvelope
	data := stringAttr(attrs, "gotel.db.query.source_map")
	if data == "" || len(data) > 16384 {
		return out, false
	}
	d := json.NewDecoder(bytes.NewBufferString(data))
	d.DisallowUnknownFields()
	if d.Decode(&out) != nil || d.Decode(new(any)) != io.EOF || out.SchemaVersion != 1 {
		return out, false
	}
	length, ok := integer(attrs["gotel.db.query.original_length"])
	if !ok || out.OriginalToSanitized.FromLength != length {
		return out, false
	}
	if _, err := out.OriginalToSanitized.MapRange(0, 0, false); err != nil {
		return out, false
	}
	switch out.Display {
	case "sanitized":
		if out.OriginalToSanitized.ToLength != len(query) {
			return out, false
		}
	case "formatted":
		if out.SanitizedToFormatted.FromLength != out.OriginalToSanitized.ToLength || out.SanitizedToFormatted.ToLength != len(query) {
			return out, false
		}
		if _, err := out.SanitizedToFormatted.MapRange(0, 0, false); err != nil {
			return out, false
		}
	default:
		return out, false
	}
	return out, true
}

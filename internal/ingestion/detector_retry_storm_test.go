package ingestion

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zfogg/spaniel/internal/ingestion/detectors"
	"github.com/zfogg/spaniel/internal/storage"
)

func retrySpans(explicit bool) []*storage.Span {
	spans := []*storage.Span{{SpanID: "parent"}}
	for i := range 3 {
		status := 2
		if i == 2 {
			status = 1
		}
		counter := ""
		if explicit {
			counter = fmt.Sprintf(`,"http.request.resend_count":%d`, i)
		}
		spans = append(spans, &storage.Span{SpanID: fmt.Sprint(i), ParentSpanID: "parent", Kind: 3, StartNs: int64(i*20 + 1), EndNs: int64(i*20 + 11), StatusCode: status, Attributes: `{"http.request.method":"GET","url.full":"https://user:secret@example.com/private?token=secret"` + counter + `}`})
	}
	return spans
}

func TestRetryStormLateEvidenceUpsertsOnce(t *testing.T) {
	db, err := storage.Open(filepath.Join(t.TempDir(), "retry.duckdb"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	d := &detectors.RetryStormDetector{}
	spans := retrySpans(false)
	spans[3].StatusCode = 2
	for _, issue := range d.Analyze("trace", "session", spans, 1) {
		if err := db.UpsertTraceIssue(issue); err != nil {
			t.Fatal(err)
		}
	}
	spans = append(spans, &storage.Span{SpanID: "late", ParentSpanID: "parent", Kind: 3, StartNs: 100, EndNs: 110, Attributes: spans[1].Attributes})
	for _, issue := range d.Analyze("trace", "session", spans, 2) {
		if err := db.UpsertTraceIssue(issue); err != nil {
			t.Fatal(err)
		}
	}
	issues, err := db.GetTraceIssues("trace")
	if err != nil || len(issues) != 1 || issues[0].Count != 4 || !strings.Contains(issues[0].Fingerprint, "109ns") {
		t.Fatalf("issues=%+v err=%v", issues, err)
	}
	_, total, err := db.ListNotifications("issue", 1, 30)
	if err != nil || total != 1 {
		t.Fatalf("notifications=%d err=%v", total, err)
	}
}

func TestRetryStormEvidenceAndReanalysis(t *testing.T) {
	d := &detectors.RetryStormDetector{}
	for _, explicit := range []bool{false, true} {
		spans := retrySpans(explicit)
		got := d.Analyze("trace", "session", spans, 1)
		if len(got) != 1 || got[0].Count != 3 || got[0].WastedNs != 0 || !strings.Contains(got[0].Fingerprint, "50ns") || got[0].ExampleSpanID != "0" || got[0].ParentSpanID != "parent" {
			t.Fatalf("unexpected evidence: %+v", got)
		}
		if strings.Contains(fmt.Sprintf("%+v", got[0]), "secret") {
			t.Fatal("sensitive URL leaked")
		}
		spans[3].StatusCode = 2
		lateAttrs := spans[1].Attributes
		if explicit {
			lateAttrs = strings.Replace(lateAttrs, `"http.request.resend_count":0`, `"http.request.resend_count":3`, 1)
		}
		spans = append(spans, &storage.Span{SpanID: "late", ParentSpanID: "parent", Kind: 3, StartNs: 100, EndNs: 110, Attributes: lateAttrs})
		again := d.Analyze("trace", "session", spans, 2)
		if len(again) != 1 || again[0].ID != got[0].ID || again[0].Count != 4 || !strings.Contains(again[0].Fingerprint, "109ns") {
			t.Fatalf("unstable identity: %+v", again)
		}
	}
}

func TestRetryStormRejectsAmbiguousSequences(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func([]*storage.Span)
	}{
		{"ordinary success", func(s []*storage.Span) { s[1].StatusCode = 1 }},
		{"parallel", func(s []*storage.Span) { s[2].StartNs = s[1].StartNs }},
		{"redirect", func(s []*storage.Span) {
			s[1].Attributes = `{"http.request.method":"GET","url.full":"https://user:secret@example.com/private?token=secret","http.response.status_code":302}`
		}},
		{"long gap", func(s []*storage.Span) { s[2].StartNs = 2_000_000_000; s[2].EndNs = 2_000_000_010 }},
		{"missing parent", func(s []*storage.Span) { s[0].SpanID = "other" }},
		{"different operation", func(s []*storage.Span) {
			s[2].Attributes = `{"http.request.method":"GET","url.full":"https://example.com/other"}`
		}},
		{"counter reset", func(s []*storage.Span) { s[2].Attributes = s[1].Attributes }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := retrySpans(true)
			tc.change(s)
			if got := (&detectors.RetryStormDetector{}).Analyze("trace", "session", s, 0); len(got) != 0 {
				t.Fatalf("unexpected: %+v", got)
			}
		})
	}
	partial := retrySpans(false)
	partial[0].Attributes = `{"spaniel.trace.partial":true}`
	if !detectors.SupportsTrace(&detectors.RetryStormDetector{}, partial) || len((&detectors.RetryStormDetector{}).Analyze("trace", "session", partial, 0)) != 0 {
		t.Fatal("partial trace should allow explicit evidence only")
	}
	if got := (&detectors.RetryStormDetector{}).Analyze("trace", "session", make([]*storage.Span, 10001), 0); len(got) != 0 {
		t.Fatal("oversized trace accepted")
	}
}

func TestRetryStormExplicitResendsWithMissingAttempts(t *testing.T) {
	d := &detectors.RetryStormDetector{}
	spans := retrySpans(true)
	spans[0].Attributes = `{"spaniel.trace.partial":true}`
	spans[3].StatusCode = 2
	spans = []*storage.Span{spans[0], spans[3]}
	got := d.Analyze("trace", "session", spans, 0)
	if len(got) != 1 || got[0].Count != 1 || !strings.Contains(got[0].Fingerprint, "3 attempts indicated (2 resends); 1 matching spans observed") || !strings.Contains(got[0].Fingerprint, "redirects may be included") {
		t.Fatalf("unexpected explicit evidence: %+v", got)
	}
	redirect := *spans[1]
	redirect.SpanID = "redirect"
	redirect.StartNs, redirect.EndNs = 1, 11
	redirect.Attributes = strings.Replace(redirect.Attributes, `"http.request.resend_count":2`, `"http.request.resend_count":1,"http.response.status_code":302`, 1)
	if got := d.Analyze("trace", "session", append(spans, &redirect), 0); len(got) != 0 {
		t.Fatalf("observed redirect reported as retry: %+v", got)
	}
	spans[1].Attributes = strings.Replace(spans[1].Attributes, `"http.request.resend_count":2`, `"http.request.resend_count":2,"http.response.status_code":302`, 1)
	if got := d.Analyze("trace", "session", spans, 0); len(got) != 0 {
		t.Fatalf("redirect reported: %+v", got)
	}
	spans[1].Attributes = strings.Replace(spans[1].Attributes, `,"http.response.status_code":302`, "", 1)
	spans[1].StatusCode = 1
	if got := d.Analyze("trace", "session", spans, 0); len(got) != 0 {
		t.Fatalf("unexplained successful resend reported: %+v", got)
	}
}

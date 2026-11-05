package detectors

import (
	"crypto/sha256"
	"fmt"
	"math"
	"net/url"
	"sort"
	"strings"
	"time"

	json "github.com/goccy/go-json"
	"github.com/zfogg/spaniel/internal/storage"
)

// RetryStormDetector reports a conservative sequence of at least three attempts.
type RetryStormDetector struct{}

func (*RetryStormDetector) Kind() string { return "retry_storm" }

func (*RetryStormDetector) Analyze(traceID, sessionID string, spans []*storage.Span, now int64) []*storage.TraceIssue {
	// Avoid analyzing a prefix: it would give misleading counts and unstable evidence.
	if len(spans) > 10000 {
		return nil
	}
	type attempt struct {
		span      *storage.Span
		failed    bool
		redirect  bool
		resend    float64
		hasResend bool
	}
	groups := map[string][]attempt{}
	parents := map[string]bool{}
	partial := false
	for _, s := range spans {
		parents[s.SpanID] = true
		if v, ok := extractAttrBool(s.Attributes, "spaniel.trace.partial"); ok && v {
			partial = true
		}
	}
	for _, s := range spans {
		if s.Kind != 3 || s.ParentSpanID == "" || !parents[s.ParentSpanID] || s.EndNs <= s.StartNs {
			continue
		}
		var a map[string]any
		if json.Unmarshal([]byte(s.Attributes), &a) != nil {
			continue
		}
		str := func(k string) string { v, _ := a[k].(string); return v }
		method := str("http.request.method")
		if method == "" {
			method = str("http.method")
		}
		raw := str("url.full")
		if raw == "" {
			raw = str("http.url")
		}
		// Exact HTTP URL matching avoids conflating different requests. Userinfo and
		// fragments are discarded; the key itself is never exposed in findings.
		u, err := url.Parse(raw)
		if method == "" || err != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") {
			continue
		}
		u.User = nil
		u.Fragment = ""
		key := s.ParentSpanID + "\x00" + strings.ToUpper(method) + "\x00" + u.String()
		code, _ := a["http.response.status_code"].(float64)
		if code == 0 {
			code, _ = a["http.status_code"].(float64)
		}
		// Redirects are resends, not evidence of a retry after failure.
		failed := (s.StatusCode == 2 || code >= 400) && !(code >= 300 && code < 400)
		resend, has := a["http.request.resend_count"].(float64)
		groups[key] = append(groups[key], attempt{s, failed, code >= 300 && code < 400, resend, has})
	}
	var issues []*storage.TraceIssue
	for key, attempts := range groups {
		sort.Slice(attempts, func(i, j int) bool {
			if attempts[i].span.StartNs == attempts[j].span.StartNs {
				return attempts[i].span.SpanID < attempts[j].span.SpanID
			}
			return attempts[i].span.StartNs < attempts[j].span.StartNs
		})
		// One finding per operation and parent. Stop at the longest valid chain;
		// independent later chains do not inflate its count.
		bestStart, bestLen, start := 0, 1, 0
		for i := 1; i < len(attempts); i++ {
			prev, cur := attempts[i-1], attempts[i]
			gap := cur.span.StartNs - prev.span.EndNs
			valid := prev.failed && gap >= 0 && gap <= 1_000_000_000
			if prev.hasResend && cur.hasResend && cur.resend != prev.resend+1 {
				valid = false
			}
			if !valid {
				start = i
			}
			if i-start+1 > bestLen {
				bestStart, bestLen = start, i-start+1
			}
		}
		if partial || bestLen < 3 {
			// Resend counts also include redirects. Treat a failed span with at
			// least two resends as possible retry evidence, never as proof that
			// missing attempts failed. Known redirects suppress this inference.
			candidate := -1
			redirect := false
			for i, a := range attempts {
				redirect = redirect || a.redirect
				if a.failed && a.hasResend && a.resend >= 2 && a.resend <= 10000 && math.Trunc(a.resend) == a.resend && (candidate < 0 || a.resend > attempts[candidate].resend) {
					candidate = i
				}
			}
			if redirect || candidate < 0 {
				continue
			}
			a := attempts[candidate]
			hash := sha256.Sum256([]byte(key))
			issues = append(issues, &storage.TraceIssue{ID: fmt.Sprintf("%s-retry_storm-%x", traceID, hash[:12]), TraceID: traceID, SessionID: sessionID, Kind: "retry_storm", Fingerprint: fmt.Sprintf("Possible retries: %d attempts indicated (%d resends); %d matching spans observed; redirects may be included", int(a.resend)+1, int(a.resend), len(attempts)), Count: len(attempts), ParentSpanID: a.span.ParentSpanID, ExampleSpanID: a.span.SpanID, CreatedAt: now})
			continue
		}
		first, last := attempts[bestStart].span, attempts[bestStart+bestLen-1].span
		hash := sha256.Sum256([]byte(key))
		issues = append(issues, &storage.TraceIssue{ID: fmt.Sprintf("%s-retry_storm-%x", traceID, hash[:12]), TraceID: traceID, SessionID: sessionID, Kind: "retry_storm", Fingerprint: fmt.Sprintf("Repeated HTTP dependency attempts; elapsed %s", time.Duration(last.EndNs-first.StartNs)), Count: bestLen, ParentSpanID: first.ParentSpanID, ExampleSpanID: first.SpanID, CreatedAt: now})
	}
	return issues
}

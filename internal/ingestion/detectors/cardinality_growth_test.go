package detectors

import (
	"testing"
	"time"
)

func TestEvaluateCardinality(t *testing.T) {
	now := int64(3 * time.Hour)
	for _, tc := range []struct {
		name string
		e    CardinalityEvidence
		want string
	}{
		{"new", CardinalityEvidence{Active: 1800, Limit: 2000, StartedAt: now - int64(time.Minute), Now: now}, "insufficient_history"},
		{"growth", CardinalityEvidence{Active: 150, OlderHalf: 25, NewerHalf: 25, Limit: 2000, StartedAt: now - int64(time.Hour), Now: now}, "growth"},
		{"burst", CardinalityEvidence{Active: 150, NewerHalf: 50, Limit: 2000, StartedAt: now - int64(time.Hour), Now: now}, "stable"},
		{"stable", CardinalityEvidence{Active: 100, Limit: 2000, StartedAt: now - int64(time.Hour), Now: now}, "stable"},
		{"limit", CardinalityEvidence{Active: 1600, Limit: 2000, StartedAt: now - int64(time.Hour), Now: now}, "approaching_limit"},
		{"no limit", CardinalityEvidence{Active: 1600, StartedAt: now - int64(time.Hour), Now: now}, "stable"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := EvaluateCardinality(tc.e); got.Status != tc.want {
				t.Fatalf("status = %q, want %q", got.Status, tc.want)
			}
		})
	}
}

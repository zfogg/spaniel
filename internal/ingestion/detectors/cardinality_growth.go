package detectors

import "time"

// CardinalityWindow is measured in admission time, not OTLP point time.
const CardinalityWindow = time.Hour

// CardinalityEvidence contains only aggregate counts and attribute keys.
type CardinalityEvidence struct {
	Active, OlderHalf, NewerHalf, Limit int
	StartedAt, Now                      int64
}

// CardinalityResult describes why a stream was or was not flagged.
type CardinalityResult struct {
	Status string
	Growth int
}

// EvaluateCardinality requires a complete hour of observation before making
// any claim. Growth must be present in both halves to exclude a single burst.
// A limit warning requires the same baseline so startup and resets are quiet.
func EvaluateCardinality(e CardinalityEvidence) CardinalityResult {
	if e.StartedAt <= 0 || e.Now-e.StartedAt < int64(CardinalityWindow) {
		return CardinalityResult{Status: "insufficient_history"}
	}
	growth := e.OlderHalf + e.NewerHalf
	base := e.Active - growth
	if base < 0 {
		return CardinalityResult{Status: "insufficient_history"}
	}
	if e.OlderHalf >= 10 && e.NewerHalf >= 10 && growth >= 50 && growth*4 >= base {
		return CardinalityResult{Status: "growth", Growth: growth}
	}
	if e.Limit > 0 && e.Active*5 >= e.Limit*4 {
		return CardinalityResult{Status: "approaching_limit", Growth: growth}
	}
	return CardinalityResult{Status: "stable", Growth: growth}
}

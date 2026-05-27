package ci

import "fmt"

// CheckOptions tunes regression sensitivity. Zero values fall back to the
// defaults documented in DefaultCheckOptions.
type CheckOptions struct {
	// DurationThresholdPct: an operation's p95 may grow by at most this much
	// before it counts as a regression. Same threshold applies to total root
	// duration. Default 20.
	DurationThresholdPct float64

	// N1Threshold: how many extra repetitions of a fingerprint count as a new
	// N+1 regression (vs. a baseline that already saw some repetition). Default 10.
	N1Threshold int
}

// DefaultCheckOptions returns the values used when CheckOptions fields are 0.
func DefaultCheckOptions() CheckOptions {
	return CheckOptions{DurationThresholdPct: 20, N1Threshold: 10}
}

func (o CheckOptions) normalize() CheckOptions {
	if o.DurationThresholdPct <= 0 {
		o.DurationThresholdPct = 20
	}
	if o.N1Threshold <= 0 {
		o.N1Threshold = 10
	}
	return o
}

// Regression is one specific reason `ci check` would fail.
type Regression struct {
	Kind        string `json:"kind"` // "duration" | "root_duration" | "n_plus_one" | "error"
	Service     string `json:"service,omitempty"`
	Operation   string `json:"operation,omitempty"`
	Fingerprint string `json:"fingerprint,omitempty"`
	BaselineP95 int64  `json:"baseline_p95_ns,omitempty"`
	CurrentP95  int64  `json:"current_p95_ns,omitempty"`
	BaselineNs  int64  `json:"baseline_ns,omitempty"`
	CurrentNs   int64  `json:"current_ns,omitempty"`
	BaselineCnt int    `json:"baseline_count,omitempty"`
	CurrentCnt  int    `json:"current_count,omitempty"`
	DeltaPct    float64 `json:"delta_pct,omitempty"`
	Message     string `json:"message"`
}

// Result is the outcome of a Compare call. Pass is true only when Regressions is empty.
type Result struct {
	Pass        bool         `json:"pass"`
	Options     CheckOptions `json:"options"`
	Regressions []Regression `json:"regressions"`
}

// Compare evaluates current against baseline and reports any regressions.
//   - Each operation's p95 may grow by at most DurationThresholdPct.
//   - The root trace duration may grow by at most DurationThresholdPct.
//   - Any N+1 fingerprint not present in the baseline is a regression.
//     Existing fingerprints are a regression when current_count exceeds
//     baseline_count + N1Threshold.
//   - Any (service, operation) producing errors in current but not in baseline.
func Compare(baseline, current Snapshot, opts CheckOptions) Result {
	opts = opts.normalize()
	res := Result{Options: opts}

	// Duration regressions per operation.
	baseOps := map[string]Operation{}
	for _, op := range baseline.Operations {
		baseOps[op.Service+"\x00"+op.Name] = op
	}
	for _, cur := range current.Operations {
		base, ok := baseOps[cur.Service+"\x00"+cur.Name]
		if !ok || base.P95Ns <= 0 {
			continue
		}
		delta := pctDelta(base.P95Ns, cur.P95Ns)
		if delta > opts.DurationThresholdPct {
			res.Regressions = append(res.Regressions, Regression{
				Kind:        "duration",
				Service:     cur.Service,
				Operation:   cur.Name,
				BaselineP95: base.P95Ns,
				CurrentP95:  cur.P95Ns,
				DeltaPct:    delta,
				Message: fmt.Sprintf("p95 of %s/%s regressed %.1f%% (%d → %d ns)",
					cur.Service, cur.Name, delta, base.P95Ns, cur.P95Ns),
			})
		}
	}

	// Root duration regression.
	if baseline.RootDurationNs > 0 {
		delta := pctDelta(baseline.RootDurationNs, current.RootDurationNs)
		if delta > opts.DurationThresholdPct {
			res.Regressions = append(res.Regressions, Regression{
				Kind:       "root_duration",
				BaselineNs: baseline.RootDurationNs,
				CurrentNs:  current.RootDurationNs,
				DeltaPct:   delta,
				Message: fmt.Sprintf("root trace duration regressed %.1f%% (%d → %d ns)",
					delta, baseline.RootDurationNs, current.RootDurationNs),
			})
		}
	}

	// N+1 regressions.
	baseN1 := map[string]int{}
	for _, n := range baseline.NPlusOne {
		baseN1[n.Fingerprint] = n.Count
	}
	for _, n := range current.NPlusOne {
		bCount, existed := baseN1[n.Fingerprint]
		switch {
		case !existed:
			res.Regressions = append(res.Regressions, Regression{
				Kind:        "n_plus_one",
				Fingerprint: n.Fingerprint,
				CurrentCnt:  n.Count,
				Message:     fmt.Sprintf("new N+1 detected (%d×): %s", n.Count, n.Fingerprint),
			})
		case n.Count > bCount+opts.N1Threshold:
			res.Regressions = append(res.Regressions, Regression{
				Kind:        "n_plus_one",
				Fingerprint: n.Fingerprint,
				BaselineCnt: bCount,
				CurrentCnt:  n.Count,
				Message: fmt.Sprintf("N+1 worsened (%d → %d): %s",
					bCount, n.Count, n.Fingerprint),
			})
		}
	}

	// Error regressions: any (svc, name) producing errors now but clean in baseline.
	baseErrs := map[string]bool{}
	for _, e := range baseline.Errors {
		baseErrs[e.Service+"\x00"+e.Name] = true
	}
	for _, e := range current.Errors {
		if !baseErrs[e.Service+"\x00"+e.Name] {
			res.Regressions = append(res.Regressions, Regression{
				Kind:       "error",
				Service:    e.Service,
				Operation:  e.Name,
				CurrentCnt: e.Count,
				Message:    fmt.Sprintf("new ERROR span: %s/%s (%d×)", e.Service, e.Name, e.Count),
			})
		}
	}

	res.Pass = len(res.Regressions) == 0
	return res
}

func pctDelta(base, cur int64) float64 {
	if base <= 0 {
		return 0
	}
	return float64(cur-base) / float64(base) * 100
}

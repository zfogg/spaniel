// Package ci builds and compares baseline snapshots for the `spaniel ci`
// subcommand. The types and algorithms here are deliberately decoupled from
// HTTP, storage, and the rest of spaniel — they take plain inputs so they
// stay easy to test.
package ci

import (
	"sort"

	"github.com/zfogg/spaniel/internal/storage"
)

const SnapshotVersion = 1

// Snapshot is the on-disk baseline format. It captures enough about a session
// for `ci check` to detect regressions without needing access to every span.
type Snapshot struct {
	Version        int         `json:"version"`
	ExportedAt     int64       `json:"exported_at_ns"`
	SessionLabel   string      `json:"session_label"`
	TraceCount     int         `json:"trace_count"`
	SpanCount      int         `json:"span_count"`
	RootDurationNs int64       `json:"root_duration_ns"`
	Operations     []Operation `json:"operations"`
	NPlusOne       []N1Group   `json:"n_plus_one"`
	Errors         []ErrorOp   `json:"errors"`
}

// Operation aggregates duration statistics for a (service, name) pair.
type Operation struct {
	Service string `json:"service"`
	Name    string `json:"name"`
	Count   int    `json:"count"`
	P50Ns   int64  `json:"p50_ns"`
	P95Ns   int64  `json:"p95_ns"`
	P99Ns   int64  `json:"p99_ns"`
}

// N1Group is a normalized DB fingerprint that fired the N+1 detector.
type N1Group struct {
	Fingerprint string `json:"fingerprint"`
	Count       int    `json:"count"`
	WastedNs    int64  `json:"wasted_ns"`
}

// ErrorOp is a (service, name) pair that produced at least one ERROR span.
type ErrorOp struct {
	Service string `json:"service"`
	Name    string `json:"name"`
	Count   int    `json:"count"`
}

// BuildSnapshot constructs a Snapshot from a session's spans and issues.
// label is the session label to embed in the snapshot. exportedAtNs is the
// timestamp to embed (so callers can use time.Now() or freeze in tests).
func BuildSnapshot(label string, exportedAtNs int64, spans []*storage.Span, issues []*storage.TraceIssue) Snapshot {
	type key struct{ svc, name string }
	durs := map[key][]int64{}
	errs := map[key]int{}
	traces := map[string]struct{}{}
	var rootDur int64

	for _, s := range spans {
		k := key{s.ServiceName, s.Name}
		durs[k] = append(durs[k], s.DurationNs)
		traces[s.TraceID] = struct{}{}
		if s.StatusCode == 2 {
			errs[k]++
		}
		if (s.ParentSpanID == "" || s.ParentSpanID == "0000000000000000") && s.DurationNs > rootDur {
			rootDur = s.DurationNs
		}
	}

	ops := make([]Operation, 0, len(durs))
	for k, ds := range durs {
		sort.Slice(ds, func(i, j int) bool { return ds[i] < ds[j] })
		ops = append(ops, Operation{
			Service: k.svc,
			Name:    k.name,
			Count:   len(ds),
			P50Ns:   percentile(ds, 50),
			P95Ns:   percentile(ds, 95),
			P99Ns:   percentile(ds, 99),
		})
	}
	sort.Slice(ops, func(i, j int) bool {
		if ops[i].Service != ops[j].Service {
			return ops[i].Service < ops[j].Service
		}
		return ops[i].Name < ops[j].Name
	})

	errOps := make([]ErrorOp, 0, len(errs))
	for k, n := range errs {
		errOps = append(errOps, ErrorOp{Service: k.svc, Name: k.name, Count: n})
	}
	sort.Slice(errOps, func(i, j int) bool {
		if errOps[i].Service != errOps[j].Service {
			return errOps[i].Service < errOps[j].Service
		}
		return errOps[i].Name < errOps[j].Name
	})

	n1 := make([]N1Group, 0, len(issues))
	for _, is := range issues {
		if is.Kind != "n_plus_one" {
			continue
		}
		n1 = append(n1, N1Group{Fingerprint: is.Fingerprint, Count: is.Count, WastedNs: is.WastedNs})
	}
	sort.Slice(n1, func(i, j int) bool { return n1[i].Fingerprint < n1[j].Fingerprint })

	return Snapshot{
		Version:        SnapshotVersion,
		ExportedAt:     exportedAtNs,
		SessionLabel:   label,
		TraceCount:     len(traces),
		SpanCount:      len(spans),
		RootDurationNs: rootDur,
		Operations:     ops,
		NPlusOne:       n1,
		Errors:         errOps,
	}
}

// percentile assumes the slice is already sorted ascending. Uses the
// nearest-rank method (1-based), which matches what most OTel UIs report.
func percentile(sorted []int64, p int) int64 {
	if len(sorted) == 0 {
		return 0
	}
	idx := (p * len(sorted)) / 100
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return sorted[idx]
}

// Package coverage computes "what fraction of an API surface has any
// instrumentation?" — see issue #18. The actual math is pure; the
// HTTP-facing wrapper lives in internal/api.
package coverage

import (
	"encoding/json"
	"sort"

	"github.com/zfogg/spaniel/internal/storage"
)

// Route is one entry on a service's operation list. Method is uppercase HTTP
// verb ("GET", "POST", ...) or "RPC" for non-HTTP operations.
type Route struct {
	Method string `json:"method"`
	Path   string `json:"path"`
	Hits   int    `json:"hits"`
	P95Ns  int64  `json:"p95_ns,omitempty"`
}

// ServiceCoverage is the per-service section of the report.
type ServiceCoverage struct {
	Name           string  `json:"name"`
	Source         string  `json:"source"`            // "openapi" | "observed"
	Spec           string  `json:"spec,omitempty"`    // filename when source=openapi
	ObservedOps    int     `json:"observed_operations"`
	TotalRoutes    int     `json:"total_routes"`
	CoveragePct    float64 `json:"coverage_pct"`
	DarkRoutes     []Route `json:"dark_routes"`
	ObservedRoutes []Route `json:"observed_routes"`
}

// Report is the top-level response for GET /api/coverage.
type Report struct {
	Services []ServiceCoverage `json:"services"`
	Overall  Overall           `json:"overall"`
}

// Overall is the rolled-up across-all-services summary.
type Overall struct {
	ObservedOps int     `json:"observed_operations"`
	TotalRoutes int     `json:"total_routes"`
	DarkCount   int     `json:"dark_count"`
	CoveragePct float64 `json:"coverage_pct"`
}

// ManifestRoute is one declared route from an imported spec.
type ManifestRoute struct {
	Method string
	Path   string
}

// Manifests maps service name → declared routes from a spec.
type Manifests struct {
	// per-service routes, plus the spec filename it came from for UI display.
	Spec   string
	Routes map[string][]ManifestRoute
}

// Compute walks the given spans, extracts observed (service, method, path)
// triples, joins them against any declared manifests, and returns the report.
// It is pure — no DB, no HTTP, easy to test.
func Compute(spans []*storage.Span, m *Manifests) Report {
	type key struct{ svc, method, path string }

	hits := map[key]int{}
	durs := map[key][]int64{}
	servicesSeen := map[string]bool{}

	for _, s := range spans {
		op := extractOperation(s)
		if op.path == "" {
			continue
		}
		k := key{s.ServiceName, op.method, op.path}
		hits[k]++
		durs[k] = append(durs[k], s.DurationNs)
		servicesSeen[s.ServiceName] = true
	}

	// Index observed routes by service.
	observedBySvc := map[string][]Route{}
	for k, h := range hits {
		ds := durs[k]
		sort.Slice(ds, func(i, j int) bool { return ds[i] < ds[j] })
		observedBySvc[k.svc] = append(observedBySvc[k.svc], Route{
			Method: k.method,
			Path:   k.path,
			Hits:   h,
			P95Ns:  percentile(ds, 95),
		})
	}

	// Make sure every service from a manifest still appears, even with 0 spans.
	if m != nil {
		for svc := range m.Routes {
			servicesSeen[svc] = true
		}
	}

	services := make([]string, 0, len(servicesSeen))
	for svc := range servicesSeen {
		services = append(services, svc)
	}
	sort.Strings(services)

	var report Report
	for _, svc := range services {
		observed := observedBySvc[svc]
		sortRoutes(observed)
		var manifestRoutes []ManifestRoute
		spec := ""
		if m != nil {
			manifestRoutes = m.Routes[svc]
			if len(manifestRoutes) > 0 {
				spec = m.Spec
			}
		}

		sc := ServiceCoverage{
			Name:           svc,
			ObservedRoutes: observed,
			ObservedOps:    len(observed),
		}
		if len(manifestRoutes) > 0 {
			sc.Source = "openapi"
			sc.Spec = spec
			// Build the dark list = manifest - observed.
			obs := make(map[string]bool, len(observed))
			for _, r := range observed {
				obs[routeKey(r.Method, r.Path)] = true
			}
			var dark []Route
			seen := map[string]bool{}
			for _, mr := range manifestRoutes {
				if obs[routeKey(mr.Method, mr.Path)] {
					continue
				}
				k := routeKey(mr.Method, mr.Path)
				if seen[k] {
					continue
				}
				seen[k] = true
				dark = append(dark, Route{Method: mr.Method, Path: mr.Path})
			}
			sortRoutes(dark)
			sc.DarkRoutes = dark
			sc.TotalRoutes = len(observed) + len(dark)
		} else {
			sc.Source = "observed"
			sc.TotalRoutes = len(observed)
			sc.DarkRoutes = []Route{}
		}
		if sc.TotalRoutes > 0 {
			sc.CoveragePct = round1(float64(sc.ObservedOps) / float64(sc.TotalRoutes) * 100)
		} else {
			sc.CoveragePct = 0
		}
		report.Services = append(report.Services, sc)
	}

	// Roll up.
	for _, sc := range report.Services {
		report.Overall.ObservedOps += sc.ObservedOps
		report.Overall.TotalRoutes += sc.TotalRoutes
		report.Overall.DarkCount += len(sc.DarkRoutes)
	}
	if report.Overall.TotalRoutes > 0 {
		report.Overall.CoveragePct = round1(float64(report.Overall.ObservedOps) / float64(report.Overall.TotalRoutes) * 100)
	}

	// Guarantee non-nil slices for stable JSON.
	if report.Services == nil {
		report.Services = []ServiceCoverage{}
	}
	return report
}

// extractOperation picks the (method, path) pair off a span. Order of
// preference: http.route + http method → RPC method → server-kind span name.
func extractOperation(s *storage.Span) struct{ method, path string } {
	var attrs map[string]any
	if err := json.Unmarshal([]byte(s.Attributes), &attrs); err != nil {
		attrs = map[string]any{}
	}

	if route, ok := stringAttr(attrs, "http.route"); ok && route != "" {
		method := firstNonEmpty(stringOrEmpty(attrs, "http.request.method"), stringOrEmpty(attrs, "http.method"))
		if method == "" {
			method = "GET"
		}
		return struct{ method, path string }{method, route}
	}
	if m, ok := stringAttr(attrs, "rpc.method"); ok && m != "" {
		svc, _ := stringAttr(attrs, "rpc.service")
		path := m
		if svc != "" {
			path = svc + "." + m
		}
		return struct{ method, path string }{"RPC", path}
	}
	// Server-kind spans without http.route still count as observed operations.
	if s.Kind == 2 && s.Name != "" {
		return struct{ method, path string }{"", s.Name}
	}
	return struct{ method, path string }{"", ""}
}

func stringAttr(m map[string]any, k string) (string, bool) {
	v, ok := m[k]
	if !ok {
		return "", false
	}
	s, ok := v.(string)
	return s, ok
}

func stringOrEmpty(m map[string]any, k string) string {
	s, _ := stringAttr(m, k)
	return s
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

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

func routeKey(method, path string) string { return method + " " + path }

func sortRoutes(rs []Route) {
	sort.Slice(rs, func(i, j int) bool {
		if rs[i].Method != rs[j].Method {
			return rs[i].Method < rs[j].Method
		}
		return rs[i].Path < rs[j].Path
	})
}

func round1(v float64) float64 {
	return float64(int64(v*10+0.5)) / 10
}

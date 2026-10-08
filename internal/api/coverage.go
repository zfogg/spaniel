package api

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/zfogg/spaniel/internal/api/apigen"
	"github.com/zfogg/spaniel/internal/coverage"
	"github.com/zfogg/spaniel/internal/storage"
)

// getCoverage returns a coverage.Report joining observed spans (optionally
// filtered to one session) against any manifests loaded at startup.
//
//	GET /api/coverage?sessionId=...
func (r *Router) getCoverage(w http.ResponseWriter, req *http.Request) {
	sessionID := req.URL.Query().Get("sessionId")
	if sessionID == "" {
		sessionID = r.store.ActiveSessionID()
	}
	operations, err := r.store.WithContext(req.Context()).ListCoverageOperations(sessionID)
	if err != nil {
		respondErr(w, req, 500, err.Error())
		return
	}
	quality, err := r.store.WithContext(req.Context()).GetCoverageQuality(sessionID)
	if err != nil {
		respondErr(w, req, 500, err.Error())
		return
	}
	observed := make([]coverage.Operation, len(operations))
	for i, operation := range operations {
		observed[i] = coverage.Operation{
			ServiceName: operation.ServiceName,
			Method:      operation.Method,
			Path:        operation.Path,
			Hits:        operation.Hits,
			P95Ns:       operation.P95Ns,
			LastSeenNs:  operation.LastSeenNs,
		}
	}
	report := coverage.ComputeOperations(observed, r.coverageManifests())
	report.Quality = coverage.Quality{MissingRouteSpans: quality.MissingRouteSpans, GenericRouteSpans: quality.GenericRouteSpans, DynamicRouteSpans: quality.DynamicRouteSpans}
	respond(w, report, len(report.Services), 1)
}

func (r *Router) coverageManifests() *coverage.Manifests {
	specs, err := r.store.ListCoverageSpecs()
	if err != nil {
		return r.manifests
	}
	m := &coverage.Manifests{}
	for _, spec := range specs {
		if !spec.Enabled {
			continue
		}
		routes, _, err := coverage.ParseOpenAPI([]byte(spec.Content), spec.ServiceName)
		if err == nil {
			m.Sources = append(m.Sources, coverage.ManifestSource{ID: spec.ID, Name: spec.Name, ServiceName: spec.ServiceName, Routes: routes})
		}
	}
	if r.manifests != nil {
		m.Spec, m.Routes = r.manifests.Spec, r.manifests.Routes
	}
	return m
}

type coverageSpecInput struct {
	Name        string `json:"name"`
	ServiceName string `json:"service_name"`
	Content     string `json:"content"`
	SourceURL   string `json:"source_url"`
	Enabled     *bool  `json:"enabled"`
}

func (r *Router) coverageSpecRoutes(mux chi.Router) {
	mux.Get("/api/coverage/specs", r.listCoverageSpecs)
	mux.Post("/api/coverage/specs", r.createCoverageSpec)
	mux.Put("/api/coverage/specs/{id}", r.replaceCoverageSpec)
	mux.Delete("/api/coverage/specs/{id}", r.deleteCoverageSpec)
}
func (r *Router) getCoverageSpec(w http.ResponseWriter, req *http.Request) {
	spec, err := r.store.WithContext(req.Context()).GetCoverageSpec(chi.URLParam(req, "id"))
	if err != nil {
		respondErr(w, req, http.StatusNotFound, "coverage spec not found")
		return
	}
	respond(w, spec, 1, 1)
}
func (r *Router) listCoverageSpecs(w http.ResponseWriter, req *http.Request) {
	xs, err := r.store.WithContext(req.Context()).ListCoverageSpecs()
	if err != nil {
		respondErr(w, req, 500, err.Error())
		return
	}
	// Documents can be large. List metadata only; GET /{id} is the explicit
	// inspect/edit operation that returns the stored source.
	for _, spec := range xs {
		spec.Content = ""
	}
	respond(w, xs, len(xs), 1)
}
func (r *Router) createCoverageSpec(w http.ResponseWriter, req *http.Request) {
	r.saveCoverageSpec(w, req, "")
}
func (r *Router) replaceCoverageSpec(w http.ResponseWriter, req *http.Request) {
	r.saveCoverageSpec(w, req, chi.URLParam(req, "id"))
}
func (r *Router) deleteCoverageSpec(w http.ResponseWriter, req *http.Request) {
	if err := r.store.WithContext(req.Context()).DeleteCoverageSpec(chi.URLParam(req, "id")); err != nil {
		respondErr(w, req, 500, err.Error())
		return
	}
	respond(w, map[string]bool{"ok": true}, 1, 1)
}
func (r *Router) saveCoverageSpec(w http.ResponseWriter, req *http.Request, id string) {
	var in coverageSpecInput
	if err := json.NewDecoder(http.MaxBytesReader(w, req.Body, 2<<20)).Decode(&in); err != nil {
		respondErr(w, req, 400, "invalid spec request: "+err.Error())
		return
	}
	in.Name, in.ServiceName = strings.TrimSpace(in.Name), strings.TrimSpace(in.ServiceName)
	if in.Name == "" || in.ServiceName == "" {
		respondErr(w, req, 400, "name and service_name are required")
		return
	}
	content := []byte(in.Content)
	if strings.TrimSpace(in.SourceURL) != "" {
		var err error
		content, err = fetchSpec(in.SourceURL)
		if err != nil {
			respondErr(w, req, 400, err.Error())
			return
		}
	}
	routes, _, err := coverage.ParseOpenAPI(content, in.ServiceName)
	if err != nil {
		respondErr(w, req, 400, err.Error())
		return
	}
	now := time.Now().UnixNano()
	enabled := true
	var spec *storage.CoverageSpec
	creating := id == ""
	if !creating {
		spec, err = r.store.GetCoverageSpec(id)
		if err != nil {
			respondErr(w, req, 404, "coverage spec not found")
			return
		}
		if in.ServiceName != spec.ServiceName {
			respondErr(w, req, 400, "service_name cannot be changed after a coverage spec is created")
			return
		}
	} else {
		spec = &storage.CoverageSpec{ID: uuid.NewString(), CreatedAt: now}
	}
	if in.Enabled != nil {
		enabled = *in.Enabled
	} else if id != "" {
		enabled = spec.Enabled
	}
	sum := sha256.Sum256(content)
	spec.Name, spec.ServiceName, spec.Content, spec.SourceURL, spec.Format, spec.Digest, spec.RouteCount, spec.Enabled, spec.UpdatedAt = in.Name, in.ServiceName, string(content), in.SourceURL, "openapi", hex.EncodeToString(sum[:]), len(routes), enabled, now
	store := r.store.WithContext(req.Context())
	if err := func() error {
		if creating {
			return store.CreateCoverageSpec(spec)
		}
		return store.UpdateCoverageSpec(spec)
	}(); err != nil {
		respondErr(w, req, 500, err.Error())
		return
	}
	respond(w, spec, 1, 1)
}
func fetchSpec(raw string) ([]byte, error) {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil {
		return nil, fmt.Errorf("source_url must be an absolute http(s) URL without credentials")
	}
	host := u.Hostname()
	// A self-monitoring Spaniel commonly imports its own live contract. Avoid
	// recursively issuing an HTTP request through the instance being updated;
	// it can contend with the single local DuckDB connection under load.
	if strings.EqualFold(host, "localhost") && u.Path == "/api/openapi.json" {
		doc, err := apigen.GetSwagger()
		if err != nil {
			return nil, fmt.Errorf("load embedded OpenAPI: %w", err)
		}
		return json.Marshal(doc)
	}
	if !strings.EqualFold(host, "localhost") {
		ips, lookupErr := net.LookupIP(host)
		if lookupErr != nil || len(ips) == 0 {
			return nil, fmt.Errorf("source_url host could not be resolved")
		}
		for _, ip := range ips {
			if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsUnspecified() {
				return nil, fmt.Errorf("source_url must not target a private network address")
			}
		}
	}
	c := &http.Client{Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return fmt.Errorf("redirects are not allowed") }}
	res, err := c.Get(u.String())
	if err != nil {
		return nil, fmt.Errorf("fetch spec: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode > 299 {
		return nil, fmt.Errorf("fetch spec: %s", res.Status)
	}
	b, err := io.ReadAll(io.LimitReader(res.Body, 2<<20+1))
	if err != nil {
		return nil, err
	}
	if len(b) > 2<<20 {
		return nil, fmt.Errorf("spec exceeds 2 MiB")
	}
	return b, nil
}

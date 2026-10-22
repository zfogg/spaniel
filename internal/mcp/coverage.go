package mcp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/zfogg/spaniel/internal/coverage"
	"github.com/zfogg/spaniel/internal/model"
)

func (h *handler) registerCoverageTools(s *mcpsdk.Server) {
	readOnly := &mcpsdk.ToolAnnotations{ReadOnlyHint: true}
	addTOONTool(s, &mcpsdk.Tool{Name: "get_coverage", Description: "Compare observed HTTP operations with enabled OpenAPI coverage specs and report covered, dark, and quality gaps.", Annotations: readOnly}, h.getCoverage)
	addTOONTool(s, &mcpsdk.Tool{Name: "list_coverage_specs", Description: "List stored OpenAPI coverage-spec metadata without returning the potentially large document bodies.", Annotations: readOnly}, h.listCoverageSpecs)
	addTOONTool(s, &mcpsdk.Tool{Name: "get_coverage_spec", Description: "Get one stored OpenAPI coverage spec including its document body.", Annotations: readOnly}, h.getCoverageSpec)
	if h.opts.AllowWrites {
		addTOONTool(s, &mcpsdk.Tool{Name: "apply_coverage_spec", Description: "Create or replace a stored OpenAPI coverage spec after parsing and validating its routes.", Annotations: &mcpsdk.ToolAnnotations{DestructiveHint: boolPtr(false), IdempotentHint: true}}, h.applyCoverageSpec)
		addTOONTool(s, &mcpsdk.Tool{Name: "delete_coverage_spec", Description: "Delete one stored coverage spec.", Annotations: &mcpsdk.ToolAnnotations{DestructiveHint: boolPtr(true)}}, h.deleteCoverageSpec)
	}
}

type GetCoverageInput struct {
	SessionID string `json:"session_id,omitempty" jsonschema:"session to inspect; defaults to active"`
}

type CoverageOutput struct {
	SessionID string          `json:"session_id"`
	Report    coverage.Report `json:"report"`
}

func (h *handler) getCoverage(ctx context.Context, _ *mcpsdk.CallToolRequest, in GetCoverageInput) (*mcpsdk.CallToolResult, CoverageOutput, error) {
	sessionID := h.resolveSession(in.SessionID)
	operations, err := h.store.WithContext(ctx).ListCoverageOperations(sessionID)
	if err != nil {
		return nil, CoverageOutput{}, fmt.Errorf("list coverage operations: %w", err)
	}
	quality, err := h.store.WithContext(ctx).GetCoverageQuality(sessionID)
	if err != nil {
		return nil, CoverageOutput{}, fmt.Errorf("get coverage quality: %w", err)
	}
	observed := make([]coverage.Operation, len(operations))
	for i, operation := range operations {
		observed[i] = coverage.Operation{ServiceName: operation.ServiceName, Method: operation.Method, Path: operation.Path, Hits: operation.Hits, P95Ns: operation.P95Ns, LastSeenNs: operation.LastSeenNs}
	}
	report := coverage.ComputeOperations(observed, h.coverageManifests())
	report.Quality = coverage.Quality{MissingRouteSpans: quality.MissingRouteSpans, GenericRouteSpans: quality.GenericRouteSpans, DynamicRouteSpans: quality.DynamicRouteSpans}
	return nil, CoverageOutput{SessionID: sessionID, Report: report}, nil
}

type ListCoverageSpecsInput struct {
	Service string `json:"service,omitempty" jsonschema:"only specs for this service.name"`
	Enabled *bool  `json:"enabled,omitempty" jsonschema:"only enabled (true) or disabled (false) specs"`
	Search  string `json:"search,omitempty" jsonschema:"case-insensitive spec-name substring"`
	Limit   int    `json:"limit,omitempty" jsonschema:"max specs to return (default 100, max 500)"`
	Page    int    `json:"page,omitempty" jsonschema:"1-based page number (default 1)"`
}

type ListCoverageSpecsOutput struct {
	Count int                   `json:"count"`
	Specs []*model.CoverageSpec `json:"specs"`
}

func (h *handler) listCoverageSpecs(ctx context.Context, _ *mcpsdk.CallToolRequest, in ListCoverageSpecsInput) (*mcpsdk.CallToolResult, ListCoverageSpecsOutput, error) {
	specs, err := h.store.WithContext(ctx).ListCoverageSpecs()
	if err != nil {
		return nil, ListCoverageSpecsOutput{}, err
	}
	limit, page := in.Limit, in.Page
	if limit <= 0 {
		limit = 100
	}
	if limit > 500 {
		limit = 500
	}
	if page < 1 {
		page = 1
	}
	matched, start := 0, (page-1)*limit
	filtered := make([]*model.CoverageSpec, 0, limit)
	for _, spec := range specs {
		if in.Service != "" && spec.ServiceName != in.Service || in.Enabled != nil && spec.Enabled != *in.Enabled || in.Search != "" && !strings.Contains(strings.ToLower(spec.Name), strings.ToLower(in.Search)) {
			continue
		}
		if matched < start {
			matched++
			continue
		}
		matched++
		spec.Content = ""
		filtered = append(filtered, spec)
		if len(filtered) >= limit {
			break
		}
	}
	return nil, ListCoverageSpecsOutput{Count: len(filtered), Specs: filtered}, nil
}

type CoverageSpecInput struct {
	SpecID string `json:"spec_id"`
}

func (h *handler) getCoverageSpec(ctx context.Context, _ *mcpsdk.CallToolRequest, in CoverageSpecInput) (*mcpsdk.CallToolResult, *model.CoverageSpec, error) {
	spec, err := h.store.WithContext(ctx).GetCoverageSpec(in.SpecID)
	if err != nil {
		return nil, nil, fmt.Errorf("coverage spec not found")
	}
	return nil, spec, nil
}

type ApplyCoverageSpecInput struct {
	SpecID            string `json:"spec_id,omitempty"`
	Name              string `json:"name"`
	ServiceName       string `json:"service_name"`
	OpenAPI           string `json:"openapi" jsonschema:"OpenAPI JSON or YAML document"`
	Enabled           *bool  `json:"enabled,omitempty"`
	ExpectedUpdatedAt string `json:"expected_updated_at,omitempty" jsonschema:"exact decimal updated_at returned by get_coverage_spec; rejects a stale replacement"`
	DryRun            bool   `json:"dry_run,omitempty"`
}

type ApplyCoverageSpecOutput struct {
	Applied bool                `json:"applied"`
	Spec    *model.CoverageSpec `json:"spec"`
}

func (h *handler) applyCoverageSpec(ctx context.Context, _ *mcpsdk.CallToolRequest, in ApplyCoverageSpecInput) (*mcpsdk.CallToolResult, ApplyCoverageSpecOutput, error) {
	in.Name, in.ServiceName = strings.TrimSpace(in.Name), strings.TrimSpace(in.ServiceName)
	if in.Name == "" || in.ServiceName == "" {
		return nil, ApplyCoverageSpecOutput{}, fmt.Errorf("name and service_name are required")
	}
	routes, _, err := coverage.ParseOpenAPI([]byte(in.OpenAPI), in.ServiceName)
	if err != nil {
		return nil, ApplyCoverageSpecOutput{}, err
	}
	now := time.Now().UnixNano()
	id, enabled, createdAt, creating := in.SpecID, true, now, in.SpecID == ""
	if creating {
		id = uuid.NewString()
	} else {
		existing, err := h.store.WithContext(ctx).GetCoverageSpec(id)
		if err != nil {
			return nil, ApplyCoverageSpecOutput{}, fmt.Errorf("coverage spec not found")
		}
		if existing.ServiceName != in.ServiceName {
			return nil, ApplyCoverageSpecOutput{}, fmt.Errorf("service_name cannot be changed after creation")
		}
		expected, err := parseExpectedUpdatedAt(in.ExpectedUpdatedAt)
		if err != nil {
			return nil, ApplyCoverageSpecOutput{}, err
		}
		if expected != 0 && existing.UpdatedAt != expected {
			return nil, ApplyCoverageSpecOutput{}, fmt.Errorf("coverage spec changed since it was read")
		}
		enabled, createdAt = existing.Enabled, existing.CreatedAt
	}
	if in.Enabled != nil {
		enabled = *in.Enabled
	}
	sum := sha256.Sum256([]byte(in.OpenAPI))
	spec := &model.CoverageSpec{ID: id, Name: in.Name, ServiceName: in.ServiceName, Format: "openapi", Content: in.OpenAPI, Digest: hex.EncodeToString(sum[:]), RouteCount: len(routes), Enabled: enabled, CreatedAt: createdAt, UpdatedAt: now}
	if in.DryRun {
		return nil, ApplyCoverageSpecOutput{Applied: false, Spec: spec}, nil
	}
	if creating {
		err = h.store.WithContext(ctx).CreateCoverageSpec(spec)
	} else {
		err = h.store.WithContext(ctx).UpdateCoverageSpec(spec)
	}
	if err != nil {
		return nil, ApplyCoverageSpecOutput{}, err
	}
	return nil, ApplyCoverageSpecOutput{Applied: true, Spec: spec}, nil
}

func (h *handler) deleteCoverageSpec(ctx context.Context, _ *mcpsdk.CallToolRequest, in CoverageSpecInput) (*mcpsdk.CallToolResult, MutationOutput, error) {
	if _, err := h.store.WithContext(ctx).GetCoverageSpec(in.SpecID); err != nil {
		return nil, MutationOutput{}, fmt.Errorf("coverage spec not found")
	}
	if err := h.store.WithContext(ctx).DeleteCoverageSpec(in.SpecID); err != nil {
		return nil, MutationOutput{}, err
	}
	return nil, MutationOutput{OK: true}, nil
}

func (h *handler) coverageManifests() *coverage.Manifests {
	specs, err := h.store.ListCoverageSpecs()
	if err != nil {
		return h.opts.Manifests
	}
	manifests := &coverage.Manifests{}
	for _, spec := range specs {
		if !spec.Enabled {
			continue
		}
		routes, _, err := coverage.ParseOpenAPI([]byte(spec.Content), spec.ServiceName)
		if err == nil {
			manifests.Sources = append(manifests.Sources, coverage.ManifestSource{ID: spec.ID, Name: spec.Name, ServiceName: spec.ServiceName, Routes: routes})
		}
	}
	if h.opts.Manifests != nil {
		manifests.Spec, manifests.Routes = h.opts.Manifests.Spec, h.opts.Manifests.Routes
	}
	return manifests
}

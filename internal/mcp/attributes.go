package mcp

import (
	"context"
	"fmt"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zfogg/spaniel/internal/model"
	"github.com/zfogg/spaniel/internal/storage"
)

type DiscoverAttributesInput struct {
	SessionID string    `json:"session_id,omitempty" jsonschema:"session to inspect; defaults to active"`
	Service   string    `json:"service,omitempty" jsonschema:"exact service.name filter"`
	Signal    string    `json:"signal,omitempty" jsonschema:"spans, logs, or metrics; omit for all"`
	Scope     string    `json:"scope,omitempty" jsonschema:"attributes or resource; omit for both. Logs only store attributes"`
	Prefix    string    `json:"prefix,omitempty" jsonschema:"literal key prefix for key discovery"`
	Key       string    `json:"key,omitempty" jsonschema:"exact attribute key; required by list_attribute_values"`
	FromNs    UnixNanos `json:"from_ns,omitempty" jsonschema:"inclusive signal timestamp lower bound as decimal unix ns"`
	ToNs      UnixNanos `json:"to_ns,omitempty" jsonschema:"inclusive signal timestamp upper bound as decimal unix ns"`
	Limit     int       `json:"limit,omitempty" jsonschema:"page size; default 50, max 200"`
	Page      int       `json:"page,omitempty" jsonschema:"1-based page; default 1"`
}
type AttributeKeysOutput struct {
	SessionID string                  `json:"session_id"`
	Keys      []model.AttributeKeyRow `json:"keys"`
	HasMore   bool                    `json:"has_more"`
}
type AttributeValuesOutput struct {
	SessionID string                    `json:"session_id"`
	Values    []model.AttributeValueRow `json:"values"`
	HasMore   bool                      `json:"has_more"`
}

func (h *handler) attributeDiscoveryFilter(in DiscoverAttributesInput) (storage.AttributeDiscoveryFilter, error) {
	if in.Signal != "" && in.Signal != "spans" && in.Signal != "logs" && in.Signal != "metrics" {
		return storage.AttributeDiscoveryFilter{}, fmt.Errorf("signal must be spans, logs, or metrics")
	}
	if in.Scope != "" && in.Scope != "attributes" && in.Scope != "resource" {
		return storage.AttributeDiscoveryFilter{}, fmt.Errorf("scope must be attributes or resource")
	}
	from, to, err := parseTimeWindow(in.FromNs, in.ToNs)
	if err != nil {
		return storage.AttributeDiscoveryFilter{}, err
	}
	limit, page := in.Limit, in.Page
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	if page < 1 {
		page = 1
	}
	if page > 1000000 {
		return storage.AttributeDiscoveryFilter{}, fmt.Errorf("page must not exceed 1000000")
	}
	return storage.AttributeDiscoveryFilter{SessionID: h.resolveSession(in.SessionID), Service: in.Service, Signal: in.Signal, Scope: in.Scope, Prefix: in.Prefix, Key: in.Key, FromNs: from, ToNs: to, Limit: limit + 1, Offset: (page - 1) * limit}, nil
}
func (h *handler) listAttributeKeys(ctx context.Context, _ *mcpsdk.CallToolRequest, in DiscoverAttributesInput) (*mcpsdk.CallToolResult, AttributeKeysOutput, error) {
	f, err := h.attributeDiscoveryFilter(in)
	if err != nil {
		return nil, AttributeKeysOutput{}, err
	}
	rows, err := h.store.WithContext(ctx).DiscoverAttributeKeys(f)
	if err != nil {
		return nil, AttributeKeysOutput{}, err
	}
	if rows == nil {
		rows = []model.AttributeKeyRow{}
	}
	more := len(rows) == f.Limit
	if more {
		rows = rows[:len(rows)-1]
	}
	return nil, AttributeKeysOutput{f.SessionID, rows, more}, nil
}
func (h *handler) listAttributeValues(ctx context.Context, _ *mcpsdk.CallToolRequest, in DiscoverAttributesInput) (*mcpsdk.CallToolResult, AttributeValuesOutput, error) {
	if in.Key == "" {
		return nil, AttributeValuesOutput{}, fmt.Errorf("key is required")
	}
	f, err := h.attributeDiscoveryFilter(in)
	if err != nil {
		return nil, AttributeValuesOutput{}, err
	}
	rows, err := h.store.WithContext(ctx).DiscoverAttributeValues(f)
	if err != nil {
		return nil, AttributeValuesOutput{}, err
	}
	if rows == nil {
		rows = []model.AttributeValueRow{}
	}
	more := len(rows) == f.Limit
	if more {
		rows = rows[:len(rows)-1]
	}
	return nil, AttributeValuesOutput{f.SessionID, rows, more}, nil
}

func (h *handler) registerAgentTools(s *mcpsdk.Server) {
	read := &mcpsdk.ToolAnnotations{ReadOnlyHint: true}
	addTOONTool(s, &mcpsdk.Tool{Name: "get_service_summary", Description: "Summarize one service in a session/time window: span latency/error counts, logs, metrics, freshness, top operations/findings, dependencies, and explicitly session-wide route coverage. Latency/error rate are span-based, not request-based.", Annotations: read}, h.getServiceSummary)
	addTOONTool(s, &mcpsdk.Tool{Name: "list_attribute_keys", Description: "Discover observed top-level attribute keys with occurrence counts by signal and scope. Scope distinguishes telemetry attributes from resource attributes. Supports service, session, time window, literal prefix and pagination.", Annotations: read}, h.listAttributeKeys)
	addTOONTool(s, &mcpsdk.Tool{Name: "list_attribute_values", Description: "Discover exact values and JSON types for an attribute key, with occurrence counts by signal/scope. Use returned values in attributes/resource_attributes filters on list_traces, list_slow_spans, query_logs (attributes only), or get_metric_series. Scalar matching uses text values; all filters must match on the same record.", Annotations: read}, h.listAttributeValues)
}

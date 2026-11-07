package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zfogg/spaniel/internal/querydsl"
	"github.com/zfogg/spaniel/internal/storage"
)

type QuerySignalInput struct {
	Attributes         map[string]string `json:"attributes,omitempty" jsonschema:"exact record attribute matches"`
	ResourceAttributes map[string]string `json:"resource_attributes,omitempty" jsonschema:"exact resource attribute matches"`
	Query              string            `json:"query,omitempty"`
	SessionID          string            `json:"session_id,omitempty" jsonschema:"session id; defaults to active"`
	Limit              int               `json:"limit,omitempty" jsonschema:"page size; default 100, maximum 1000"`
	Page               int               `json:"page,omitempty" jsonschema:"1-based page; default 1"`
}
type QuerySignalOutput struct {
	SessionID string           `json:"session_id"`
	Page      int              `json:"page"`
	Limit     int              `json:"limit"`
	Total     int64            `json:"total"`
	HasMore   bool             `json:"has_more"`
	Rows      []map[string]any `json:"rows"`
}

func (h *handler) registerSignalQueries(s *mcpsdk.Server) {
	for _, signal := range []string{"spans", "traces", "metrics"} {
		signal := signal
		addTOONTool(s, &mcpsdk.Tool{Name: "query_" + signal, Description: "Search " + signal + " in a telemetry session.", Annotations: &mcpsdk.ToolAnnotations{ReadOnlyHint: true}}, func(ctx context.Context, _ *mcpsdk.CallToolRequest, in QuerySignalInput) (*mcpsdk.CallToolResult, QuerySignalOutput, error) {
			expr, err := querydsl.Parse(in.Query)
			if err != nil {
				return nil, QuerySignalOutput{}, err
			}
			expr = addAttributeFilters(expr, in.Attributes, in.ResourceAttributes)
			if in.Limit <= 0 {
				in.Limit = 100
			}
			if in.Limit > 1000 {
				in.Limit = 1000
			}
			if in.Page <= 0 {
				in.Page = 1
			}
			session := h.resolveSession(in.SessionID)
			result, err := h.store.WithContext(ctx).SearchSignals(storage.SignalSearch{Signal: signal, SessionID: session, Expr: expr, Limit: in.Limit, Page: in.Page})
			if err != nil {
				return nil, QuerySignalOutput{}, err
			}
			data, err := json.Marshal(result.Rows)
			if err != nil {
				return nil, QuerySignalOutput{}, err
			}
			dec := json.NewDecoder(bytes.NewReader(data))
			dec.UseNumber()
			rows := []map[string]any{}
			if err = dec.Decode(&rows); err != nil {
				return nil, QuerySignalOutput{}, fmt.Errorf("encode search results: %w", err)
			}
			if rows == nil {
				rows = []map[string]any{}
			}
			out := QuerySignalOutput{SessionID: session, Page: in.Page, Limit: in.Limit, Total: result.Total, HasMore: int64(in.Page)*int64(in.Limit) < result.Total, Rows: rows}
			return nil, out, nil
		})
	}
}

func addAttributeFilters(expr *querydsl.Node, attributes, resources map[string]string) *querydsl.Node {
	for prefix, values := range map[string]map[string]string{"attributes.": attributes, "resource.": resources} {
		for key, value := range values {
			term := &querydsl.Node{Op: "=", Field: prefix + key, Value: value}
			if expr == nil {
				expr = term
			} else {
				expr = &querydsl.Node{Op: "and", Left: expr, Right: term}
			}
		}
	}
	return expr
}

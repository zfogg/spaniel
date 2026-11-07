package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"reflect"
	"strconv"
	"strings"

	"errors"
	"fmt"
	"github.com/google/jsonschema-go/jsonschema"
	"sync"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	toongo "github.com/toon-format/toon-go"

	"github.com/zfogg/spaniel/internal/errtext"
	"github.com/zfogg/spaniel/internal/sqlformat"
	"github.com/zfogg/spaniel/internal/storage"
)

var registeredToolNames sync.Map

// addTOONTool returns compact TOON text alongside schema-validated structured data.
func addTOONTool[In, Out any](s *mcpsdk.Server, tool *mcpsdk.Tool, handler mcpsdk.ToolHandlerFor[In, Out]) {
	registeredToolNames.Store(tool.Name, struct{}{})
	if tool.OutputSchema == nil {
		typ := reflect.TypeFor[Out]()
		if typ.Kind() == reflect.Pointer {
			typ = typ.Elem()
		}
		schema, err := jsonschema.ForType(typ, &jsonschema.ForOptions{})
		if err != nil {
			panic(err)
		}
		raw, err := json.Marshal(schema)
		if err != nil {
			panic(err)
		}
		var value any
		if err := json.Unmarshal(raw, &value); err != nil {
			panic(err)
		}
		widenIntegerSchemas(value)
		tool.OutputSchema = value
	}
	mcpsdk.AddTool[In, any](s, tool, func(ctx context.Context, req *mcpsdk.CallToolRequest, in In) (*mcpsdk.CallToolResult, any, error) {
		var zero any
		result, out, err := handler(ctx, req, in)
		if err != nil {
			message := errtext.Message(err.Error())
			code := "tool_error"
			var retryable *bool
			switch {
			case errors.Is(err, context.DeadlineExceeded):
				code = "deadline_exceeded"
				transient := true
				retryable = &transient
			case errors.Is(err, context.Canceled):
				code = "cancelled"
			case errors.Is(err, storage.ErrStorageFull):
				code = "storage_full"
			}
			return &mcpsdk.CallToolResult{IsError: true, Content: []mcpsdk.Content{&mcpsdk.TextContent{Text: message}}, StructuredContent: map[string]any{"code": code, "message": message, "retryable": retryable}}, zero, nil
		}
		data, err := toongo.Marshal(sqlformat.Presentation(out))
		if err != nil {
			return nil, zero, fmt.Errorf("encode TOON result: %w", err)
		}
		if result == nil {
			result = &mcpsdk.CallToolResult{}
		}
		result.Content = []mcpsdk.Content{&mcpsdk.TextContent{Text: string(data)}}
		raw, err := json.Marshal(out)
		if err != nil {
			return nil, nil, fmt.Errorf("encode structured result: %w", err)
		}
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.UseNumber()
		var structured any
		if err := decoder.Decode(&structured); err != nil {
			return nil, nil, err
		}
		return result, preserveLargeIntegers(structured), nil
	})
}

// addTextTool is for outputs whose native format is intentional, such as YAML
// exports. It still uses the SDK's typed JSON Schema input inference without
// adding JSON structuredContent to the response.
func addTextTool[In any](s *mcpsdk.Server, tool *mcpsdk.Tool, handler func(context.Context, *mcpsdk.CallToolRequest, In) (string, error)) {
	registeredToolNames.Store(tool.Name, struct{}{})
	mcpsdk.AddTool[In, any](s, tool, func(ctx context.Context, req *mcpsdk.CallToolRequest, in In) (*mcpsdk.CallToolResult, any, error) {
		text, err := handler(ctx, req, in)
		if err != nil {
			return nil, nil, errors.New(errtext.Message(err.Error()))
		}
		return &mcpsdk.CallToolResult{Content: []mcpsdk.Content{&mcpsdk.TextContent{Text: text}}}, nil, nil
	})
}

// JavaScript and many MCP clients cannot represent integers above 2^53 exactly.
// Keep those values as decimal strings and advertise both representations.
func preserveLargeIntegers(v any) any {
	switch x := v.(type) {
	case json.Number:
		n := string(x)
		if !strings.ContainsAny(n, ".eE") {
			if i, err := strconv.ParseInt(n, 10, 64); err == nil {
				if i > 9007199254740991 || i < -9007199254740991 {
					return n
				}
			} else if u, err := strconv.ParseUint(n, 10, 64); err == nil && u > 9007199254740991 {
				return n
			}
		}
	case map[string]any:
		for k, child := range x {
			x[k] = preserveLargeIntegers(child)
		}
	case []any:
		for i, child := range x {
			x[i] = preserveLargeIntegers(child)
		}
	}
	return v
}

func widenIntegerSchemas(v any) {
	switch x := v.(type) {
	case map[string]any:
		if kind, ok := x["type"].(string); ok && kind == "integer" {
			x["type"] = []string{"integer", "string"}
		} else if kinds, ok := x["type"].([]any); ok {
			hasInteger, hasString := false, false
			for _, kind := range kinds {
				if kind == "integer" {
					hasInteger = true
				}
				if kind == "string" {
					hasString = true
				}
			}
			if hasInteger && !hasString {
				x["type"] = append(kinds, "string")
			}
		}
		for _, child := range x {
			widenIntegerSchemas(child)
		}
	case []any:
		for _, child := range x {
			widenIntegerSchemas(child)
		}
	}
}

package mcp

import (
	"context"
	"errors"
	"fmt"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	toongo "github.com/toon-format/toon-go"

	"github.com/zfogg/spaniel/internal/errtext"
)

// addTOONTool keeps MCP's JSON Schema input contract while returning the
// semantic result exactly once as TOON text. The SDK's typed AddTool helper
// normally duplicates every output as JSON structuredContent and JSON text;
// using any as the wire output suppresses that duplicate representation.
func addTOONTool[In, Out any](s *mcpsdk.Server, tool *mcpsdk.Tool, handler mcpsdk.ToolHandlerFor[In, Out]) {
	mcpsdk.AddTool[In, any](s, tool, func(ctx context.Context, req *mcpsdk.CallToolRequest, in In) (*mcpsdk.CallToolResult, any, error) {
		result, out, err := handler(ctx, req, in)
		if err != nil {
			return result, nil, errors.New(errtext.Message(err.Error()))
		}
		data, err := toongo.Marshal(out)
		if err != nil {
			return nil, nil, fmt.Errorf("encode TOON result: %w", err)
		}
		if result == nil {
			result = &mcpsdk.CallToolResult{}
		}
		result.Content = []mcpsdk.Content{&mcpsdk.TextContent{Text: string(data)}}
		result.StructuredContent = nil
		return result, nil, nil
	})
}

// addTextTool is for outputs whose native format is intentional, such as YAML
// exports. It still uses the SDK's typed JSON Schema input inference without
// adding JSON structuredContent to the response.
func addTextTool[In any](s *mcpsdk.Server, tool *mcpsdk.Tool, handler func(context.Context, *mcpsdk.CallToolRequest, In) (string, error)) {
	mcpsdk.AddTool[In, any](s, tool, func(ctx context.Context, req *mcpsdk.CallToolRequest, in In) (*mcpsdk.CallToolResult, any, error) {
		text, err := handler(ctx, req, in)
		if err != nil {
			return nil, nil, errors.New(errtext.Message(err.Error()))
		}
		return &mcpsdk.CallToolResult{Content: []mcpsdk.Content{&mcpsdk.TextContent{Text: text}}}, nil, nil
	})
}

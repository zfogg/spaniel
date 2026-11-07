package mcp

import (
	"context"
	"errors"
	"fmt"
	"sync"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	toongo "github.com/toon-format/toon-go"

	"github.com/zfogg/spaniel/internal/errtext"
	"github.com/zfogg/spaniel/internal/sqlformat"
)

var registeredToolNames sync.Map

// addTOONTool returns compact TOON text alongside schema-validated structured data.
func addTOONTool[In, Out any](s *mcpsdk.Server, tool *mcpsdk.Tool, handler mcpsdk.ToolHandlerFor[In, Out]) {
	registeredToolNames.Store(tool.Name, struct{}{})
	mcpsdk.AddTool[In, Out](s, tool, func(ctx context.Context, req *mcpsdk.CallToolRequest, in In) (*mcpsdk.CallToolResult, Out, error) {
		var zero Out
		result, out, err := handler(ctx, req, in)
		if err != nil {
			return result, zero, errors.New(errtext.Message(err.Error()))
		}
		data, err := toongo.Marshal(sqlformat.Presentation(out))
		if err != nil {
			return nil, zero, fmt.Errorf("encode TOON result: %w", err)
		}
		if result == nil {
			result = &mcpsdk.CallToolResult{}
		}
		result.Content = []mcpsdk.Content{&mcpsdk.TextContent{Text: string(data)}}
		return result, out, nil
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

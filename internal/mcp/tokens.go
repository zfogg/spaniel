package mcp

import (
	"context"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zfogg/spaniel/internal/security"
)

func (h *handler) registerTokenTools(s *mcpsdk.Server) {
	if h.opts.Security == nil {
		return
	}
	addTOONTool(s, &mcpsdk.Tool{Name: "list_tokens", Description: "List API token metadata. Never returns secrets.", Annotations: &mcpsdk.ToolAnnotations{ReadOnlyHint: true}}, func(ctx context.Context, req *mcpsdk.CallToolRequest, in struct{}) (*mcpsdk.CallToolResult, struct {
		Tokens []security.Token `json:"tokens"`
	}, error) { return nil, struct {
		Tokens []security.Token `json:"tokens"`
	}{Tokens: h.opts.Security.List()}, nil })
	if !h.opts.AllowWrites {
		return
	}
	addTOONTool(s, &mcpsdk.Tool{Name: "create_token", Description: "Create a named API token. Returns its secret once only; store it securely. Default expiry is one year. Requires MCP writes."}, func(ctx context.Context, req *mcpsdk.CallToolRequest, in security.CreateInput) (*mcpsdk.CallToolResult, security.Created, error) {
		out, err := h.opts.Security.Create(ctx, in)
		return nil, out, err
	})
	addTOONTool(s, &mcpsdk.Tool{Name: "delete_token", Description: "Revoke an API token and its sessions immediately. Requires MCP writes.", Annotations: &mcpsdk.ToolAnnotations{DestructiveHint: boolPtr(true)}}, func(ctx context.Context, req *mcpsdk.CallToolRequest, in struct {
		ID string `json:"id" jsonschema:"token ID to revoke"`
	}) (*mcpsdk.CallToolResult, map[string]bool, error) {
		err := h.opts.Security.Delete(ctx, in.ID)
		return nil, map[string]bool{"ok": err == nil}, err
	})
}

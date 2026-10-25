package mcp

import (
	"context"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	sequel "github.com/zfogg/sequel/go"
)

type FormatSQLInput struct {
	SQL     string         `json:"sql" jsonschema:"SQL text to format without executing it"`
	Dialect sequel.Dialect `json:"dialect,omitempty" jsonschema:"duckdb (default), postgresql, mysql, sqlite, clickhouse, or generic"`
}
type FormatSQLOutput struct {
	SQL string `json:"sql"`
}

func (h *handler) formatSQL(_ context.Context, _ *mcpsdk.CallToolRequest, in FormatSQLInput) (*mcpsdk.CallToolResult, FormatSQLOutput, error) {
	if in.Dialect == "" {
		in.Dialect = sequel.DialectDuckDB
	}
	sql, err := sequel.FormatSQL(in.Dialect, in.SQL)
	return nil, FormatSQLOutput{SQL: sql}, err
}

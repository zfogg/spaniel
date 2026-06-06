package mcp

import (
	"context"
	"fmt"
	"strings"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// sqlQueryTimeout bounds how long a single query may run.
const sqlQueryTimeout = 30 * time.Second

// registerSQLTool adds the read-only raw-SQL tool.
func (h *handler) registerSQLTool(s *mcpsdk.Server) {
	mcpsdk.AddTool(s, &mcpsdk.Tool{
		Name: "query_sql",
		Description: "Run SQL directly against Spaniel's DuckDB (read-only) and get back columns + rows. " +
			"Use this for ad-hoc analysis the other tools don't cover — custom aggregations, joins, group-bys, window functions. " +
			"Tables: spans, logs, metrics, sessions, trace_issues, lint_warnings, span_events, span_links, meta. " +
			"The attributes/resource columns are JSON stored as VARCHAR (use DuckDB's json functions or ::JSON). " +
			"Run `SHOW TABLES` or `DESCRIBE <table>` to inspect the schema. " +
			"Read-only is enforced by the DuckDB engine: any write, DDL, COPY, or ATTACH is rejected. " +
			"Results are capped at max_rows (default 1000).",
		Annotations: &mcpsdk.ToolAnnotations{ReadOnlyHint: true},
	}, h.querySQL)
}

type QuerySQLInput struct {
	SQL     string `json:"sql" jsonschema:"the SQL to run (read-only; the engine rejects writes)"`
	MaxRows int    `json:"max_rows,omitempty" jsonschema:"max rows to return (default 1000, max 50000)"`
}

type QuerySQLOutput struct {
	Columns   []string `json:"columns"`
	Rows      [][]any  `json:"rows"`
	RowCount  int      `json:"row_count"`
	Truncated bool     `json:"truncated"`
}

func (h *handler) querySQL(ctx context.Context, _ *mcpsdk.CallToolRequest, in QuerySQLInput) (*mcpsdk.CallToolResult, QuerySQLOutput, error) {
	if strings.TrimSpace(in.SQL) == "" {
		return nil, QuerySQLOutput{}, fmt.Errorf("sql is required")
	}
	maxRows := in.MaxRows
	if maxRows <= 0 {
		maxRows = 1000
	}
	if maxRows > 50000 {
		maxRows = 50000
	}

	qctx, cancel := context.WithTimeout(ctx, sqlQueryTimeout)
	defer cancel()

	// Read-only is enforced by the DuckDB engine (the store opens a read-only
	// connection); a write/DDL/COPY/ATTACH comes back here as an error.
	cols, rows, truncated, err := h.store.ReadOnlyQuery(qctx, in.SQL, maxRows)
	if err != nil {
		return nil, QuerySQLOutput{}, fmt.Errorf("query failed: %w", err)
	}
	if cols == nil {
		cols = []string{}
	}
	if rows == nil {
		rows = [][]any{}
	}
	return nil, QuerySQLOutput{
		Columns:   cols,
		Rows:      rows,
		RowCount:  len(rows),
		Truncated: truncated,
	}, nil
}

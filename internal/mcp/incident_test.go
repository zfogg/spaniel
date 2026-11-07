package mcp

import (
	"context"
	"testing"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestIncidentInvestigationUsesEvidenceAndSession(t *testing.T) {
	cs, db := connect(t, Options{})
	session := db.ActiveSessionID()
	seedTrace(t, db, session)
	out := callAgentTool[IncidentInvestigationOutput](t, cs, "investigate_incident", map[string]any{"service": "postgres", "session_id": session, "from_ns": "1000000", "to_ns": "3000000"})
	if out.SessionID != session || len(out.Warnings) == 0 {
		t.Fatalf("scope or uncertainty missing: %#v", out)
	}
	var foundError, foundTrace bool
	for _, observation := range out.Observations {
		if observation.Kind == "span_errors" && observation.Count == 1 {
			foundError = true
		}
		if observation.Kind == "slow_span" && observation.TraceID == testTraceID && observation.NextArguments["session_id"] == session {
			foundTrace = true
		}
	}
	if !foundError || !foundTrace {
		t.Fatalf("missing grounded evidence: %#v", out.Observations)
	}
	empty := callAgentTool[IncidentInvestigationOutput](t, cs, "investigate_incident", map[string]any{"service": "absent", "from_ns": "1000000", "to_ns": "3000000"})
	if len(empty.Warnings) == 0 {
		t.Fatal("empty evidence needs a warning")
	}
	result, err := cs.CallTool(context.Background(), &mcpsdk.CallToolParams{Name: "investigate_incident", Arguments: map[string]any{"from_ns": "20", "to_ns": "10"}})
	if err == nil && !result.IsError {
		t.Fatal("invalid window accepted")
	}
}

package mcp

import (
	"context"
	"fmt"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// registerWriteTools adds the mutating tools. It is only called when
// Options.AllowWrites is set, so without --mcp-allow-writes these tools are not
// registered at all and never appear in tools/list (issue #103).
func (h *handler) registerWriteTools(s *mcpsdk.Server) {
	additive := &mcpsdk.ToolAnnotations{DestructiveHint: boolPtr(false)}
	idempotent := &mcpsdk.ToolAnnotations{DestructiveHint: boolPtr(false), IdempotentHint: true}

	addTOONTool(s, &mcpsdk.Tool{
		Name:        "create_session",
		Description: "Create a new session (a recording bucket for telemetry). Optionally mark it as a baseline and/or activate it so incoming spans are recorded into it. Returns the new session.",
		Annotations: additive,
	}, h.createSession)

	addTOONTool(s, &mcpsdk.Tool{
		Name:        "activate_session",
		Description: "Make a session the active one, so newly received telemetry is recorded into it. Use list_sessions to find ids.",
		Annotations: idempotent,
	}, h.activateSession)

	addTOONTool(s, &mcpsdk.Tool{
		Name:        "set_baseline",
		Description: "Set or clear the baseline flag on a session. Baselines are the 'before' side for diff_sessions.",
		Annotations: idempotent,
	}, h.setBaseline)

}

func boolPtr(b bool) *bool { return &b }

// ---- create_session ----

type CreateSessionInput struct {
	Label    string `json:"label,omitempty" jsonschema:"human-readable label; defaults to a timestamp"`
	Baseline bool   `json:"baseline,omitempty" jsonschema:"mark the new session as a baseline"`
	Activate bool   `json:"activate,omitempty" jsonschema:"make this the active session so new telemetry records into it"`
}

type CreateSessionOutput struct {
	ID         string `json:"id"`
	Label      string `json:"label"`
	IsBaseline bool   `json:"is_baseline"`
	IsActive   bool   `json:"is_active"`
}

func (h *handler) createSession(ctx context.Context, _ *mcpsdk.CallToolRequest, in CreateSessionInput) (*mcpsdk.CallToolResult, CreateSessionOutput, error) {
	label := in.Label
	if label == "" {
		label = time.Now().Format("session_2006-01-02_15:04")
	}
	sess, err := h.store.CreateSession(label, in.Baseline)
	if err != nil {
		return nil, CreateSessionOutput{}, fmt.Errorf("create session: %w", err)
	}
	if in.Activate {
		h.store.SetActiveSession(sess.ID, sess.Label)
	}
	return nil, CreateSessionOutput{
		ID:         sess.ID,
		Label:      sess.Label,
		IsBaseline: sess.IsBaseline,
		IsActive:   in.Activate,
	}, nil
}

// ---- activate_session ----

type ActivateSessionInput struct {
	SessionID string `json:"session_id" jsonschema:"id of the session to activate"`
}

type OKOutput struct {
	OK                 bool   `json:"ok"`
	ActiveSessionID    string `json:"active_session_id,omitempty"`
	ActiveSessionLabel string `json:"active_session_label,omitempty"`
}

func (h *handler) activateSession(ctx context.Context, _ *mcpsdk.CallToolRequest, in ActivateSessionInput) (*mcpsdk.CallToolResult, OKOutput, error) {
	if in.SessionID == "" {
		return nil, OKOutput{}, fmt.Errorf("session_id is required")
	}
	sess, err := h.store.WithContext(ctx).GetSession(in.SessionID)
	if err != nil {
		return nil, OKOutput{}, fmt.Errorf("get session: %w", err)
	}
	if sess == nil {
		return nil, OKOutput{}, fmt.Errorf("session %q not found", in.SessionID)
	}
	h.store.SetActiveSession(sess.ID, sess.Label)
	return nil, OKOutput{OK: true, ActiveSessionID: sess.ID, ActiveSessionLabel: sess.Label}, nil
}

// ---- set_baseline ----

type SetBaselineInput struct {
	SessionID  string `json:"session_id" jsonschema:"id of the session"`
	IsBaseline bool   `json:"is_baseline" jsonschema:"true to mark as baseline, false to clear"`
}

func (h *handler) setBaseline(ctx context.Context, _ *mcpsdk.CallToolRequest, in SetBaselineInput) (*mcpsdk.CallToolResult, OKOutput, error) {
	if in.SessionID == "" {
		return nil, OKOutput{}, fmt.Errorf("session_id is required")
	}
	if err := h.store.SetBaseline(in.SessionID, in.IsBaseline); err != nil {
		return nil, OKOutput{}, fmt.Errorf("set baseline: %w", err)
	}
	return nil, OKOutput{OK: true}, nil
}

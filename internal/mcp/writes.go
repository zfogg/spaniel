package mcp

import (
	"context"
	"fmt"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/zfogg/spaniel/internal/storage"
)

// registerWriteTools adds the mutating tools. It is only called when
// Options.AllowWrites is set, so without --mcp-allow-writes these tools are not
// registered at all and never appear in tools/list (issue #103).
func (h *handler) registerWriteTools(s *mcpsdk.Server) {
	additive := &mcpsdk.ToolAnnotations{DestructiveHint: boolPtr(false)}
	idempotent := &mcpsdk.ToolAnnotations{DestructiveHint: boolPtr(false), IdempotentHint: true}
	destructive := &mcpsdk.ToolAnnotations{DestructiveHint: boolPtr(true)}

	mcpsdk.AddTool(s, &mcpsdk.Tool{
		Name:        "create_session",
		Description: "Create a new session (a recording bucket for telemetry). Optionally mark it as a baseline and/or activate it so incoming spans are recorded into it. Returns the new session.",
		Annotations: additive,
	}, h.createSession)

	mcpsdk.AddTool(s, &mcpsdk.Tool{
		Name:        "activate_session",
		Description: "Make a session the active one, so newly received telemetry is recorded into it. Use list_sessions to find ids.",
		Annotations: idempotent,
	}, h.activateSession)

	mcpsdk.AddTool(s, &mcpsdk.Tool{
		Name:        "set_baseline",
		Description: "Set or clear the baseline flag on a session. Baselines are the 'before' side for diff_sessions.",
		Annotations: idempotent,
	}, h.setBaseline)

	mcpsdk.AddTool(s, &mcpsdk.Tool{
		Name:        "prune_data",
		Description: "Apply a retention policy now, deleting old sessions to free space. Provide any of retention_days, max_sessions, max_db_size_mb (omitted/0 = no limit for that dimension). The active session is never deleted. Returns what was removed. This is destructive.",
		Annotations: destructive,
	}, h.pruneData)
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
	sess, err := h.store.GetSession(in.SessionID)
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

// ---- prune_data ----

type PruneDataInput struct {
	RetentionDays int `json:"retention_days,omitempty" jsonschema:"delete sessions older than N days (0 = no age limit)"`
	MaxSessions   int `json:"max_sessions,omitempty" jsonschema:"keep at most N sessions (0 = unlimited)"`
	MaxDBSizeMB   int `json:"max_db_size_mb,omitempty" jsonschema:"shrink to at most N MB on disk (0 = unlimited)"`
}

type PruneDataOutput struct {
	DeletedByAge     int   `json:"deleted_by_age"`
	DeletedByCount   int   `json:"deleted_by_count"`
	DeletedBySize    int   `json:"deleted_by_size"`
	FinalSessions    int   `json:"final_sessions"`
	FinalDBSizeBytes int64 `json:"final_db_size_bytes"`
}

func (h *handler) pruneData(ctx context.Context, _ *mcpsdk.CallToolRequest, in PruneDataInput) (*mcpsdk.CallToolResult, PruneDataOutput, error) {
	cfg := storage.RetentionConfig{MaxSessions: in.MaxSessions}
	if in.RetentionDays > 0 {
		cfg.MaxAge = time.Duration(in.RetentionDays) * 24 * time.Hour
	}
	if in.MaxDBSizeMB > 0 {
		cfg.MaxDBSizeBytes = int64(in.MaxDBSizeMB) * 1024 * 1024
	}
	res, err := h.store.Prune(cfg, h.store.ActiveSessionID())
	if err != nil {
		return nil, PruneDataOutput{}, fmt.Errorf("prune: %w", err)
	}
	return nil, PruneDataOutput{
		DeletedByAge:     res.DeletedByAge,
		DeletedByCount:   res.DeletedByCount,
		DeletedBySize:    res.DeletedBySize,
		FinalSessions:    res.FinalSessions,
		FinalDBSizeBytes: res.FinalDBSizeBytes,
	}, nil
}

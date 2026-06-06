package mcp

import (
	"testing"
)

var writeToolNames = []string{"create_session", "activate_session", "set_baseline", "prune_data"}

// Without AllowWrites the mutating tools must NOT be registered — they must not
// appear in tools/list and cannot be called.
func TestWriteTools_HiddenWithoutAllowWrites(t *testing.T) {
	cs, _ := connect(t, Options{Version: "t", AllowWrites: false})
	names := toolNames(t, cs)
	for _, w := range writeToolNames {
		if names[w] {
			t.Errorf("write tool %q is registered without --mcp-allow-writes", w)
		}
	}
	// A read tool is still present.
	if !names["list_traces"] {
		t.Error("expected read tools to remain registered")
	}
}

func TestWriteTools_PresentWithAllowWrites(t *testing.T) {
	cs, _ := connect(t, Options{Version: "t", AllowWrites: true})
	names := toolNames(t, cs)
	for _, w := range writeToolNames {
		if !names[w] {
			t.Errorf("write tool %q not registered with AllowWrites", w)
		}
	}
}

func TestCreateSession(t *testing.T) {
	cs, store := connect(t, Options{Version: "t", AllowWrites: true})

	out := callStructured[CreateSessionOutput](t, cs, "create_session", map[string]any{
		"label": "my-run", "baseline": true, "activate": true,
	})
	if out.Label != "my-run" {
		t.Errorf("label = %q, want my-run", out.Label)
	}
	if !out.IsBaseline {
		t.Error("expected is_baseline true")
	}
	if !out.IsActive {
		t.Error("expected is_active true")
	}
	if store.ActiveSessionID() != out.ID {
		t.Errorf("active session = %q, want %q", store.ActiveSessionID(), out.ID)
	}
}

func TestActivateSession(t *testing.T) {
	cs, store := connect(t, Options{Version: "t", AllowWrites: true})
	other, _ := store.CreateSession("other", false)

	out := callStructured[OKOutput](t, cs, "activate_session", map[string]any{"session_id": other.ID})
	if !out.OK || out.ActiveSessionID != other.ID {
		t.Errorf("activate result = %+v", out)
	}
	if store.ActiveSessionID() != other.ID {
		t.Errorf("active = %q, want %q", store.ActiveSessionID(), other.ID)
	}

	if !callToolIsError(t, cs, "activate_session", map[string]any{"session_id": "nope"}) {
		t.Error("expected error activating missing session")
	}
}

func TestSetBaseline(t *testing.T) {
	cs, store := connect(t, Options{Version: "t", AllowWrites: true})
	sess, _ := store.CreateSession("s", false)

	out := callStructured[OKOutput](t, cs, "set_baseline", map[string]any{
		"session_id": sess.ID, "is_baseline": true,
	})
	if !out.OK {
		t.Fatal("expected ok")
	}
	got, _ := store.GetSession(sess.ID)
	if got == nil || !got.IsBaseline {
		t.Errorf("session not marked baseline: %+v", got)
	}
}

func TestPruneData(t *testing.T) {
	cs, store := connect(t, Options{Version: "t", AllowWrites: true})
	// connect() created the active session; add a few more.
	for _, l := range []string{"old1", "old2", "old3"} {
		if _, err := store.CreateSession(l, false); err != nil {
			t.Fatalf("create %s: %v", l, err)
		}
	}
	active := store.ActiveSessionID()

	out := callStructured[PruneDataOutput](t, cs, "prune_data", map[string]any{"max_sessions": 1})
	if out.DeletedByCount < 1 {
		t.Errorf("expected some sessions deleted by count, got %+v", out)
	}
	// The active session must survive pruning.
	if got, _ := store.GetSession(active); got == nil {
		t.Error("active session was deleted by prune")
	}
}

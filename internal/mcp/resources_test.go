package mcp

import (
	"context"
	"strings"
	"testing"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestResources_Listed(t *testing.T) {
	cs, _ := connect(t, Options{Version: "t"})
	res, err := cs.ListResources(context.Background(), nil)
	if err != nil {
		t.Fatalf("list resources: %v", err)
	}
	uris := map[string]bool{}
	for _, r := range res.Resources {
		uris[r.URI] = true
	}
	if !uris[schemaURI] || !uris[guideURI] {
		t.Errorf("expected schema+guide resources, got %v", uris)
	}
}

func TestResource_Schema(t *testing.T) {
	cs, _ := connect(t, Options{Version: "t"})
	out, err := cs.ReadResource(context.Background(), &mcpsdk.ReadResourceParams{URI: schemaURI})
	if err != nil {
		t.Fatalf("read schema: %v", err)
	}
	if len(out.Contents) == 0 {
		t.Fatal("no contents")
	}
	text := out.Contents[0].Text
	// Tables exist from migrations even with no data.
	if out.Contents[0].MIMEType != "text/toon" {
		t.Errorf("schema MIME type = %q, want text/toon", out.Contents[0].MIMEType)
	}
	for _, want := range []string{"table_name", "spans", "trace_id", "logs"} {
		if !strings.Contains(text, want) {
			t.Errorf("schema missing %q:\n%s", want, text)
		}
	}
}

func TestResource_Guide(t *testing.T) {
	cs, _ := connect(t, Options{Version: "t"})
	out, err := cs.ReadResource(context.Background(), &mcpsdk.ReadResourceParams{URI: guideURI})
	if err != nil {
		t.Fatalf("read guide: %v", err)
	}
	if !strings.Contains(out.Contents[0].Text, "Data model") {
		t.Errorf("guide missing expected content")
	}
}

func TestPrompts_Listed(t *testing.T) {
	cs, _ := connect(t, Options{Version: "t"})
	res, err := cs.ListPrompts(context.Background(), nil)
	if err != nil {
		t.Fatalf("list prompts: %v", err)
	}
	names := map[string]bool{}
	for _, p := range res.Prompts {
		names[p.Name] = true
	}
	for _, want := range []string{"investigate_incident", "compare_session_to_baseline", "draft_observability_pack"} {
		if !names[want] {
			t.Errorf("prompt %q not listed; have %v", want, names)
		}
	}
	if len(res.Prompts) != 3 {
		t.Errorf("listed %d prompts, want exactly 3", len(res.Prompts))
	}
}

func TestCompareSessionToBaselineToolMirrorsPrompt(t *testing.T) {
	cs, _ := connect(t, Options{Version: "t"})
	out := callStructured[WorkflowPromptOutput](t, cs, "compare_session_to_baseline", map[string]any{
		"baseline": "before",
		"compare":  "after",
	})
	if out.Description != "Compare a session to a baseline" {
		t.Fatalf("description = %q", out.Description)
	}
	for _, want := range []string{"use baseline \"before\"", "use comparison session \"after\"", "diff_sessions", "get_coverage"} {
		if !strings.Contains(out.Instructions, want) {
			t.Errorf("tool instructions missing %q:\n%s", want, out.Instructions)
		}
	}

	prompt, err := cs.GetPrompt(context.Background(), &mcpsdk.GetPromptParams{
		Name:      "compare_session_to_baseline",
		Arguments: map[string]string{"baseline": "before", "compare": "after"},
	})
	if err != nil {
		t.Fatalf("get prompt: %v", err)
	}
	promptText := prompt.Messages[0].Content.(*mcpsdk.TextContent).Text
	if promptText != out.Instructions {
		t.Fatalf("tool and prompt diverged:\ntool:\n%s\nprompt:\n%s", out.Instructions, promptText)
	}
}

func TestPrompt_InvestigateIncidentFocus(t *testing.T) {
	cs, _ := connect(t, Options{Version: "t"})
	out, err := cs.GetPrompt(context.Background(), &mcpsdk.GetPromptParams{
		Name:      "investigate_incident",
		Arguments: map[string]string{"focus": "checkout"},
	})
	if err != nil {
		t.Fatalf("get prompt: %v", err)
	}
	if len(out.Messages) == 0 {
		t.Fatal("no messages")
	}
	tc, ok := out.Messages[0].Content.(*mcpsdk.TextContent)
	if !ok {
		t.Fatalf("expected TextContent, got %T", out.Messages[0].Content)
	}
	if !strings.Contains(tc.Text, "checkout") {
		t.Errorf("prompt did not weave in service arg:\n%s", tc.Text)
	}
	if !strings.Contains(tc.Text, "get_trace") {
		t.Errorf("prompt should reference get_trace:\n%s", tc.Text)
	}
}

func TestPrompt_DraftObservabilityPack(t *testing.T) {
	cs, _ := connect(t, Options{Version: "t"})
	out, err := cs.GetPrompt(context.Background(), &mcpsdk.GetPromptParams{Name: "draft_observability_pack", Arguments: map[string]string{"goal": "catch checkout regressions"}})
	if err != nil {
		t.Fatalf("get prompt: %v", err)
	}
	tc := out.Messages[0].Content.(*mcpsdk.TextContent)
	if !strings.Contains(tc.Text, "validate_dashboard") || !strings.Contains(tc.Text, "guidance, not permission") {
		t.Errorf("expected validation and safety guidance:\n%s", tc.Text)
	}
}

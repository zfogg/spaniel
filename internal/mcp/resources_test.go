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
	for _, want := range []string{"spans:", "trace_id", "logs:"} {
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
	for _, want := range []string{"debug_latest_trace", "diff_against_baseline", "find_bottlenecks"} {
		if !names[want] {
			t.Errorf("prompt %q not listed; have %v", want, names)
		}
	}
}

func TestPrompt_DebugLatestTrace_ServiceArg(t *testing.T) {
	cs, _ := connect(t, Options{Version: "t"})
	out, err := cs.GetPrompt(context.Background(), &mcpsdk.GetPromptParams{
		Name:      "debug_latest_trace",
		Arguments: map[string]string{"service": "checkout"},
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

func TestPrompt_FindBottlenecks(t *testing.T) {
	cs, _ := connect(t, Options{Version: "t"})
	out, err := cs.GetPrompt(context.Background(), &mcpsdk.GetPromptParams{Name: "find_bottlenecks"})
	if err != nil {
		t.Fatalf("get prompt: %v", err)
	}
	tc := out.Messages[0].Content.(*mcpsdk.TextContent)
	if !strings.Contains(tc.Text, "list_issues") {
		t.Errorf("expected list_issues reference:\n%s", tc.Text)
	}
}

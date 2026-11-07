package mcp

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestPreserveLargeIntegers(t *testing.T) {
	input := map[string]any{"small": json.Number("42"), "large": json.Number("1794074157898417900"), "negative": json.Number("-9007199254740992"), "float": json.Number("1.25")}
	got := preserveLargeIntegers(input).(map[string]any)
	if got["small"] != json.Number("42") || got["large"] != "1794074157898417900" || got["negative"] != "-9007199254740992" || got["float"] != json.Number("1.25") {
		t.Fatalf("unexpected result: %#v", got)
	}
	schema := map[string]any{"properties": map[string]any{"revision": map[string]any{"type": "integer"}}}
	widenIntegerSchemas(schema)
	revision := schema["properties"].(map[string]any)["revision"].(map[string]any)
	if !reflect.DeepEqual(revision["type"], []string{"integer", "string"}) {
		t.Fatalf("unexpected schema: %#v", revision)
	}
}

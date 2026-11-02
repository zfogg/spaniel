package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestTokenCLIUsesAuthenticatedAPIAndPrintsSecretOnce(t *testing.T) {
	t.Setenv("TEST_SPANIEL_TOKEN", "example-admin")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer example-admin" {
			t.Error("missing bearer header")
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.Method {
		case "POST":
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["name"] != "backend" || body["no_expiry"] != true {
				t.Error(body)
			}
			_, _ = w.Write([]byte(`{"data":{"token":{"id":"example-id"},"secret":"example-created-secret"}}`))
		case "GET":
			_, _ = w.Write([]byte(`{"data":[{"id":"example-id","name":"backend","status":"active"}]}`))
		case "DELETE":
			_, _ = w.Write([]byte(`{"data":{"ok":true}}`))
		}
	}))
	defer srv.Close()
	for _, args := range [][]string{{"create", "--name", "backend", "--no-expiry"}, {"list", "--json"}, {"delete", "example-id"}} {
		cmd := tokenSubcommand()
		var out, diagnostics bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetErr(&diagnostics)
		cmd.SetArgs(append(args, "--api", srv.URL, "--bearer-token-env", "TEST_SPANIEL_TOKEN"))
		if err := cmd.Execute(); err != nil {
			t.Fatal(err)
		}
		if args[0] == "create" {
			if strings.Count(out.String(), "example-created-secret") != 1 {
				t.Fatal(out.String())
			}
		} else if strings.Contains(out.String(), "secret") {
			t.Fatal("non-create command exposed secret")
		}
	}
	cmd := tokenSubcommand()
	cmd.SetArgs([]string{"create", "--name", "backend", "--no-expiry", "--expires-at", "2027-11-02T12:00:00Z"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("accepted conflicting expiry options")
	}
}

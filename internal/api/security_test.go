package api

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zfogg/spaniel/internal/security"
)

func TestSecurityAPIEnableRecoveryAndIsolation(t *testing.T) {
	router, settings, telemetryDB := newSettingsRouter(t)
	svc, err := security.Open(filepath.Join(t.TempDir(), "security.duckdb"), "example-encryption-password")
	if err != nil {
		t.Fatal(err)
	}
	defer svc.Close()
	settings.Security = svc
	settings.SecurityEnabledSource = "config"
	settings.SecurityPasswordSource = "config"
	settings.Viper.Set("encryption_password", "example-encryption-password")
	h := svc.HTTP(router, true)
	call := func(method, path, body, token string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		if token != "" {
			r.Header.Set("Authorization", "Bearer "+token)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	if w := call("PUT", "/api/security", `{"enabled":true}`, ""); w.Code != 409 {
		t.Fatal(w.Code, w.Body.String())
	}
	w := call("POST", "/api/security/tokens", `{"name":"backend"}`, "")
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var created struct {
		Data security.Created `json:"data"`
	}
	if err = json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	secret := created.Data.Secret
	if secret == "" {
		t.Fatal("missing one-time token")
	}
	if w := call("PUT", "/api/security", `{"enabled":true}`, secret); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := call("GET", "/api/settings", "", ""); w.Code != 401 || !strings.Contains(w.Body.String(), "token_required") {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := call("GET", "/api/security/tokens", "", secret); w.Code != 200 || strings.Contains(w.Body.String(), secret) {
		t.Fatal("list leaked secret", w.Code)
	}
	// Even the explicit MCP SQL-write path has no handle on the private DB.
	for _, query := range []string{"SELECT * FROM api_tokens", "SELECT * FROM security_keys", "ATTACH 'security.duckdb' AS private"} {
		if _, _, _, err := telemetryDB.WriteEnabledQuery(context.Background(), query, 10); err == nil {
			t.Fatal("SQL crossed security boundary", query)
		}
	}
	if w := call("DELETE", "/api/settings/data?reset=true", "", secret); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if _, code := svc.Validate(secret); code != "" {
		t.Fatal("telemetry reset deleted token", code)
	}
	// File/flag recovery publishes disabled mode on restart; no token is needed
	// to create a replacement while explicitly running with security disabled.
	if err = svc.Configure(false, "", nil); err != nil {
		t.Fatal(err)
	}
	w = call("POST", "/api/security/tokens", `{"name":"replacement","no_expiry":true}`, "")
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if err = json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if w = call("PUT", "/api/security", `{"enabled":true}`, created.Data.Secret); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w = call("GET", "/api/settings", "", created.Data.Secret); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	settings.SecurityEnabledSource = "environment"
	if w = call("PUT", "/api/security", `{"enabled":false}`, created.Data.Secret); w.Code != 409 {
		t.Fatal("override was silently changed", w.Code)
	}
	if w = call("GET", "/api/security", "", created.Data.Secret); strings.Contains(w.Body.String(), "example-encryption-password") {
		t.Fatal("status exposed password")
	}
}

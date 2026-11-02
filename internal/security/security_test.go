package security

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func testService(t *testing.T) *Service {
	t.Helper()
	s, err := Open(":memory:", "test-password-at-least-16")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}
func createTestToken(t *testing.T, s *Service, name string) Created {
	t.Helper()
	v, err := s.Create(context.Background(), CreateInput{Name: name})
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestTokenLifecycleAndExpiry(t *testing.T) {
	s := testService(t)
	now := time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return now }
	a := createTestToken(t, s, "backend")
	if a.Token.ExpiresAt == nil || !a.Token.ExpiresAt.Equal(now.AddDate(1, 0, 0)) {
		t.Fatal("default expiry is not one calendar year")
	}
	if _, code := s.Validate(a.Secret); code != "" {
		t.Fatal(code)
	}
	list, _ := json.Marshal(s.List())
	if bytes.Contains(list, []byte(a.Secret)) {
		t.Fatal("list exposed secret")
	}
	if err := s.Configure(true, "", nil); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete(context.Background(), a.Token.ID); err == nil {
		t.Fatal("deleted final usable token")
	}
	b := createTestToken(t, s, "collector")
	if err := s.Delete(context.Background(), a.Token.ID); err != nil {
		t.Fatal(err)
	}
	if _, code := s.Validate(a.Secret); code != "token_revoked" {
		t.Fatal(code)
	}
	now = now.AddDate(1, 0, 0)
	if _, code := s.Validate(b.Secret); code != "token_expired" {
		t.Fatal(code)
	}
	for _, in := range []CreateInput{{}, {Name: "bad", ExpiresAt: &now}, {Name: "bad", NoExpiry: true, ExpiresAt: &now}} {
		if _, err := s.Create(context.Background(), in); err == nil {
			t.Fatalf("accepted invalid request: %+v", in)
		}
	}
	c, err := s.Create(context.Background(), CreateInput{Name: "no expiry", NoExpiry: true})
	if err != nil || c.Token.ExpiresAt != nil {
		t.Fatal("no expiry", err)
	}
}

func TestEncryptedStoreRotationAndRecovery(t *testing.T) {
	path := filepath.Join(t.TempDir(), "security.duckdb")
	old := "old-password-at-least-16"
	next := "next-password-at-least-16"
	s, err := Open(path, old)
	if err != nil {
		t.Fatal(err)
	}
	token := createTestToken(t, s, "backend")
	if err = s.Configure(false, next, func() error { return errors.New("disk full") }); err == nil {
		t.Fatal("ignored persistence failure")
	}
	_ = s.Close()
	s, err = Open(path, old)
	if err != nil {
		t.Fatalf("old config cannot recover failed rotation: %v", err)
	}
	if err = s.Configure(false, next, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	_ = s.Close()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(data, []byte(token.Secret)) || bytes.Contains(data, []byte(next)) {
		t.Fatal("plaintext secret in database")
	}
	if wrong, err := Open(path, old); err == nil {
		_ = wrong.Close()
		t.Fatal("old password still works")
	}
	s, err = Open(path, next)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, code := s.Validate(token.Secret); code != "" {
		t.Fatal(code)
	}
	e := s.entries[token.Token.ID]
	e.row.Secret[len(e.row.Secret)-1] ^= 1
	if _, err := s.decryptToken(e.row); err == nil {
		t.Fatal("accepted tampered ciphertext")
	}
	e = s.entries[token.Token.ID]
	e.row.ExpiresAt += int64(time.Hour)
	if _, err := s.decryptToken(e.row); err == nil {
		t.Fatal("accepted tampered expiry")
	}
}

func TestHTTPBoundaryAndCookiePrecedence(t *testing.T) {
	s := testService(t)
	a := createTestToken(t, s, "admin")
	_ = s.Configure(true, "", nil)
	h := s.HTTP(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/security/session" {
			s.Session(w, r)
			return
		}
		w.WriteHeader(200)
	}), true)
	for _, path := range []string{"/api/settings", "/api/sql", "/ws", "/mcp", "/mcp/", "/debug/pprof/", "/v1/traces"} {
		for _, tc := range []struct {
			token string
			want  int
			code  string
		}{{"", 401, "token_required"}, {"wrong", 401, "token_invalid"}, {a.Secret, 200, ""}} {
			r := httptest.NewRequest(http.MethodGet, path, nil)
			if tc.token != "" {
				r.Header.Set("Authorization", "Bearer "+tc.token)
			}
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != tc.want || tc.code != "" && !strings.Contains(w.Body.String(), tc.code) {
				t.Fatalf("%s: got %d %s", path, w.Code, w.Body.String())
			}
		}
	}
	for _, path := range []string{"/settings/security", "/assets/app.js", "/api/health"} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != 200 {
			t.Fatal(path, w.Code)
		}
	}
	w := httptest.NewRecorder()
	s.Session(w, httptest.NewRequest("POST", "/api/security/session", strings.NewReader(`{"token":"`+a.Secret+`"}`)))
	if w.Code != 204 {
		t.Fatal(w.Code, w.Body.String())
	}
	cookie := w.Result().Cookies()[0]
	if !cookie.HttpOnly || cookie.Value == a.Secret || cookie.SameSite != http.SameSiteStrictMode {
		t.Fatal("unsafe session cookie")
	}
	r := httptest.NewRequest("GET", "/api/settings", nil)
	r.AddCookie(cookie)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatal(w.Code)
	}
	r.Header.Set("Authorization", "Bearer wrong")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 401 {
		t.Fatal("invalid explicit bearer fell back to cookie")
	}
	r = httptest.NewRequest("PUT", "/api/settings", nil)
	r.AddCookie(cookie)
	r.Header.Set("Origin", "https://attacker.example")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("cookie mutation missing CSRF guard")
	}
	r = httptest.NewRequest("OPTIONS", "/mcp", nil)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 204 {
		t.Fatal("preflight dispatched protected handler")
	}
	otlp := s.HTTP(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) }), false)
	for _, path := range []string{"/v1/traces", "/v1/metrics", "/v1/logs", "/api/health"} {
		r = httptest.NewRequest("POST", path, nil)
		r.AddCookie(cookie)
		w = httptest.NewRecorder()
		otlp.ServeHTTP(w, r)
		if w.Code != 401 {
			t.Fatal("OTLP accepted cookie or exempted route", path)
		}
	}
}

func TestRevocationCancelsExistingStream(t *testing.T) {
	s := testService(t)
	a := createTestToken(t, s, "client")
	_ = createTestToken(t, s, "admin")
	_ = s.Configure(true, "", nil)
	entered := make(chan struct{})
	done := make(chan struct{})
	h := s.HTTP(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { close(entered); <-r.Context().Done() }), true)
	r := httptest.NewRequest("GET", "/mcp", nil)
	r.Header.Set("Authorization", "Bearer "+a.Secret)
	go func() { h.ServeHTTP(httptest.NewRecorder(), r); close(done) }()
	<-entered
	if err := s.Delete(context.Background(), a.Token.ID); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("revoked stream remains connected")
	}
}

func TestGRPCAuthentication(t *testing.T) {
	s := testService(t)
	a := createTestToken(t, s, "collector")
	_ = s.Configure(true, "", nil)
	called := false
	handler := func(context.Context, any) (any, error) { called = true; return nil, nil }
	for _, method := range []string{"TraceService/Export", "MetricsService/Export", "LogsService/Export"} {
		for _, secret := range []string{"", "wrong", a.Secret} {
			called = false
			ctx := context.Background()
			if secret != "" {
				ctx = metadata.NewIncomingContext(ctx, metadata.Pairs("authorization", "Bearer "+secret))
			}
			_, err := s.UnaryInterceptor()(ctx, nil, &grpc.UnaryServerInfo{FullMethod: method}, handler)
			if secret == a.Secret {
				if err != nil || !called {
					t.Fatal(err)
				}
			} else if status.Code(err) != codes.Unauthenticated || called {
				t.Fatal("unauthenticated export executed", err)
			}
		}
	}
}

func TestHotEnableDisconnectsAnonymousStreams(t *testing.T) {
	s := testService(t)
	_ = createTestToken(t, s, "admin")
	entered := make(chan struct{})
	done := make(chan struct{})
	h := s.HTTP(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { close(entered); <-r.Context().Done() }), true)
	go func() { h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/ws", nil)); close(done) }()
	<-entered
	if err := s.Configure(true, "", nil); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("anonymous stream survived enabling protection")
	}
}

func TestProxyOriginAndSecureSessionCookie(t *testing.T) {
	s := testService(t)
	a := createTestToken(t, s, "admin")
	s.SetPublicURL("https://telemetry.example.com")
	r := httptest.NewRequest("POST", "http://telemetry.example.com/api/security/session", strings.NewReader(`{"token":"`+a.Secret+`"}`))
	r.Header.Set("Origin", "https://telemetry.example.com")
	w := httptest.NewRecorder()
	s.Session(w, r)
	if w.Code != 204 {
		t.Fatal(w.Code, w.Body.String())
	}
	if !w.Result().Cookies()[0].Secure {
		t.Fatal("proxy HTTPS cookie missing Secure")
	}
	r = httptest.NewRequest("POST", "http://telemetry.example.com/api/security/session", strings.NewReader(`{"token":"`+a.Secret+`"}`))
	r.Header.Set("Origin", "https://attacker.example.com")
	w = httptest.NewRecorder()
	s.Session(w, r)
	if w.Code != 403 {
		t.Fatal("untrusted origin was accepted")
	}
}

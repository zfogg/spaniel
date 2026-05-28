package receiver

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

const testToken = "s3cr3t-tok3n"

// ── BearerMiddleware (OTLP receiver) ─────────────────────────────────────────

func TestBearerMiddleware401WithoutToken(t *testing.T) {
	h := BearerMiddleware(testToken)(okHandler())
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/v1/traces", nil))
	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
	if w.Header().Get("WWW-Authenticate") == "" {
		t.Error("expected WWW-Authenticate header")
	}
}

func TestBearerMiddleware401WithWrongToken(t *testing.T) {
	h := BearerMiddleware(testToken)(okHandler())
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/traces", nil)
	req.Header.Set("Authorization", "Bearer wrongtoken")
	h.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestBearerMiddleware200WithToken(t *testing.T) {
	h := BearerMiddleware(testToken)(okHandler())
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/traces", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestBearerMiddlewarePassesCORSPreflight(t *testing.T) {
	h := BearerMiddleware(testToken)(okHandler())
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodOptions, "/v1/traces", nil))
	if w.Code != http.StatusOK {
		t.Errorf("expected OPTIONS to pass, got %d", w.Code)
	}
}

// ── APIBearerMiddleware (UI + API mux) ────────────────────────────────────────

func TestAPIBearerMiddlewareStaticAlwaysAllowed(t *testing.T) {
	h := APIBearerMiddleware(testToken)(okHandler())
	for _, path := range []string{"/", "/assets/app.js", "/favicon.ico"} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		if w.Code != http.StatusOK {
			t.Errorf("%s: expected 200, got %d", path, w.Code)
		}
	}
}

func TestAPIBearerMiddlewareHealthAlwaysAllowed(t *testing.T) {
	h := APIBearerMiddleware(testToken)(okHandler())
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/health", nil))
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestAPIBearerMiddlewareHealthSetsCookieOnValidToken(t *testing.T) {
	h := APIBearerMiddleware(testToken)(okHandler())
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var found bool
	for _, c := range w.Result().Cookies() {
		if c.Name == tokenCookie && c.Value == testToken && c.HttpOnly {
			found = true
		}
	}
	if !found {
		t.Error("expected HttpOnly spaniel_token cookie to be set")
	}
}

func TestAPIBearerMiddlewareHealthDoesNotSetCookieOnWrongToken(t *testing.T) {
	h := APIBearerMiddleware(testToken)(okHandler())
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	req.Header.Set("Authorization", "Bearer wrongtoken")
	h.ServeHTTP(w, req)
	for _, c := range w.Result().Cookies() {
		if c.Name == tokenCookie {
			t.Error("cookie should not be set for wrong token")
		}
	}
}

func TestAPIBearerMiddlewareAPI401WithoutToken(t *testing.T) {
	h := APIBearerMiddleware(testToken)(okHandler())
	for _, path := range []string{"/api/traces", "/api/sessions", "/ws"} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		if w.Code != http.StatusUnauthorized {
			t.Errorf("%s: expected 401, got %d", path, w.Code)
		}
	}
}

func TestAPIBearerMiddlewareAPI200WithHeader(t *testing.T) {
	h := APIBearerMiddleware(testToken)(okHandler())
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/traces", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestAPIBearerMiddlewareAPI200WithCookie(t *testing.T) {
	h := APIBearerMiddleware(testToken)(okHandler())
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/traces", nil)
	req.AddCookie(&http.Cookie{Name: tokenCookie, Value: testToken})
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

// ── UnaryBearerInterceptor (gRPC) ─────────────────────────────────────────────

func TestUnaryBearerInterceptor401WithoutToken(t *testing.T) {
	interceptor := UnaryBearerInterceptor(testToken)
	_, err := interceptor(context.Background(), nil, &grpc.UnaryServerInfo{}, func(_ context.Context, _ any) (any, error) {
		return "ok", nil
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if status.Code(err) != codes.Unauthenticated {
		t.Errorf("expected Unauthenticated, got %v", status.Code(err))
	}
}

func TestUnaryBearerInterceptor200WithToken(t *testing.T) {
	interceptor := UnaryBearerInterceptor(testToken)
	md := metadata.Pairs("authorization", "Bearer "+testToken)
	ctx := metadata.NewIncomingContext(context.Background(), md)
	resp, err := interceptor(ctx, nil, &grpc.UnaryServerInfo{}, func(_ context.Context, _ any) (any, error) {
		return "ok", nil
	})
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if resp != "ok" {
		t.Errorf("expected 'ok', got %v", resp)
	}
}

func TestUnaryBearerInterceptor401WithWrongToken(t *testing.T) {
	interceptor := UnaryBearerInterceptor(testToken)
	md := metadata.Pairs("authorization", "Bearer wrong")
	ctx := metadata.NewIncomingContext(context.Background(), md)
	_, err := interceptor(ctx, nil, &grpc.UnaryServerInfo{}, func(_ context.Context, _ any) (any, error) {
		return "ok", nil
	})
	if status.Code(err) != codes.Unauthenticated {
		t.Errorf("expected Unauthenticated, got %v", status.Code(err))
	}
}

func okHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
}

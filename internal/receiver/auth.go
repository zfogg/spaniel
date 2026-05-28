package receiver

import (
	"context"
	"net/http"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

const tokenCookie = "spaniel_token"

// BearerMiddleware rejects HTTP requests that don't carry a matching
// Authorization: Bearer header. Intended for OTLP receivers where there is no
// browser/cookie flow.
func BearerMiddleware(token string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodOptions {
				next.ServeHTTP(w, r)
				return
			}
			if !checkBearer(r, token) {
				w.Header().Set("WWW-Authenticate", `Bearer realm="spaniel"`)
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// APIBearerMiddleware wraps the UI+API mux. Static files and /api/health are
// exempt. /api/health seeds the spaniel_token cookie when a valid
// Authorization header is presented. All other /api/* and /ws paths require
// either the Authorization header or the cookie.
func APIBearerMiddleware(token string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			path := r.URL.Path

			if r.Method == http.MethodOptions {
				next.ServeHTTP(w, r)
				return
			}

			// Static UI files are always allowed.
			if !strings.HasPrefix(path, "/api/") && path != "/ws" {
				next.ServeHTTP(w, r)
				return
			}

			// /api/health: set cookie when valid token presented, then always allow.
			if path == "/api/health" {
				if extractBearer(r) == token {
					http.SetCookie(w, &http.Cookie{
						Name:     tokenCookie,
						Value:    token,
						Path:     "/",
						HttpOnly: true,
						SameSite: http.SameSiteStrictMode,
						Expires:  time.Now().Add(365 * 24 * time.Hour),
					})
				}
				next.ServeHTTP(w, r)
				return
			}

			// All other /api/* and /ws require header or cookie.
			if !checkBearerOrCookie(r, token) {
				w.Header().Set("WWW-Authenticate", `Bearer realm="spaniel"`)
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// UnaryBearerInterceptor is a gRPC server interceptor that validates the
// "authorization" metadata field (value: "Bearer <token>").
func UnaryBearerInterceptor(token string) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		if !checkGRPCToken(ctx, token) {
			return nil, status.Error(codes.Unauthenticated, "invalid or missing bearer token")
		}
		return handler(ctx, req)
	}
}

func checkBearer(r *http.Request, token string) bool {
	return extractBearer(r) == token
}

func checkBearerOrCookie(r *http.Request, token string) bool {
	if checkBearer(r, token) {
		return true
	}
	c, err := r.Cookie(tokenCookie)
	return err == nil && c.Value == token
}

func extractBearer(r *http.Request) string {
	v := r.Header.Get("Authorization")
	after, ok := strings.CutPrefix(v, "Bearer ")
	if !ok {
		return ""
	}
	return after
}

func checkGRPCToken(ctx context.Context, token string) bool {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return false
	}
	for _, v := range md.Get("authorization") {
		after, ok := strings.CutPrefix(v, "Bearer ")
		if ok && after == token {
			return true
		}
	}
	return false
}

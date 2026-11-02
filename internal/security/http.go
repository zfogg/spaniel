package security

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

const CookieName = "spaniel_session"

func (s *Service) Authenticated(r *http.Request) bool {
	_, code, _ := s.requestIdentity(r, true)
	return code == ""
}

func Error(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": message, "code": code})
}
func unauthorized(w http.ResponseWriter, code string) {
	challenge := `Bearer realm="spaniel"`
	if code != "token_required" {
		challenge += `, error="invalid_token"`
	}
	w.Header().Set("WWW-Authenticate", challenge)
	message := map[string]string{"token_required": "An API token is required.", "token_invalid": "The API token is invalid.", "token_expired": "The API token has expired.", "token_revoked": "The API token has been revoked."}[code]
	Error(w, http.StatusUnauthorized, code, message)
}

func sameOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	u, err := url.Parse(origin)
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	return err == nil && u.Scheme == scheme && u.Host == r.Host
}

func (s *Service) SetPublicURL(raw string) {
	u, err := url.Parse(raw)
	if err == nil && u.Host != "" && (u.Scheme == "http" || u.Scheme == "https") {
		s.publicURL.Store(u)
	} else {
		s.publicURL.Store(nil)
	}
}
func (s *Service) sameOrigin(r *http.Request) bool {
	if sameOrigin(r) {
		return true
	}
	expected := s.publicURL.Load()
	if expected == nil {
		return false
	}
	u, err := url.Parse(r.Header.Get("Origin"))
	return err == nil && u.Scheme == expected.Scheme && u.Host == expected.Host
}
func (s *Service) secureCookie(r *http.Request) bool {
	expected := s.publicURL.Load()
	return r.TLS != nil || expected != nil && expected.Scheme == "https" && expected.Host == r.Host
}
func (s *Service) requestIdentity(r *http.Request, cookies bool) (string, string, time.Time) {
	if values, ok := r.Header["Authorization"]; ok {
		if len(values) != 1 || !strings.HasPrefix(values[0], "Bearer ") {
			return "", "token_invalid", time.Time{}
		}
		id, code := s.Validate(strings.TrimPrefix(values[0], "Bearer "))
		if code == "token_required" {
			code = "token_invalid"
		}
		return id, code, time.Time{}
	}
	if cookies {
		if c, err := r.Cookie(CookieName); err == nil {
			s.mu.Lock()
			defer s.mu.Unlock()
			sess, ok := s.sessions[c.Value]
			if !ok {
				return "", "token_invalid", time.Time{}
			}
			if !s.now().Before(sess.expires) {
				delete(s.sessions, c.Value)
				return "", "token_expired", time.Time{}
			}
			e, ok := s.entries[sess.tokenID]
			if !ok {
				return "", "token_revoked", time.Time{}
			}
			return sess.tokenID, s.validLocked(e), sess.expires
		}
	}
	return "", "token_required", time.Time{}
}

// HTTP guards dynamic handlers; main=true permits only the SPA assets, minimal
// health response and credential exchange. OTLP ports have no public routes.
func (s *Service) HTTP(next http.Handler, main bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		if main {
			if path == "/api/security/session" {
				next.ServeHTTP(w, r)
				return
			}
			if r.Method == http.MethodGet && path == "/api/health" {
				next.ServeHTTP(w, r)
				return
			}
			protected := path == "/api" || strings.HasPrefix(path, "/api/") || path == "/ws" || path == "/mcp" || strings.HasPrefix(path, "/mcp/") || strings.HasPrefix(path, "/debug/") || strings.HasPrefix(path, "/v1/")
			if !protected {
				next.ServeHTTP(w, r)
				return
			}
		}
		// A preflight can return metadata only, never dispatch to a handler.
		if r.Method == http.MethodOptions {
			if !s.sameOrigin(r) {
				Error(w, 403, "origin_forbidden", "Cross-origin access is not allowed.")
				return
			}
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
			w.WriteHeader(204)
			return
		}
		id, code, deadline := s.requestIdentity(r, main)
		if s.Enabled() && code != "" {
			unauthorized(w, code)
			return
		}
		// Check WebSocket origins even with a bearer header; cookie-authenticated
		// HTTP mutations also need CSRF protection.
		if s.Enabled() && (path == "/ws" || r.Header.Get("Authorization") == "" && r.Method != http.MethodGet && r.Method != http.MethodHead) && !s.sameOrigin(r) {
			Error(w, 403, "origin_forbidden", "Cross-origin access is not allowed.")
			return
		}
		ctx, cancel := context.WithCancel(r.Context())
		defer cancel()
		go s.watch(ctx, cancel, id, deadline)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (s *Service) watch(ctx context.Context, cancel context.CancelFunc, id string, deadline time.Time) {
	for {
		s.mu.Lock()
		changed := s.changes
		e, ok := s.entries[id]
		valid := ok && s.validLocked(e) == ""
		enabled := s.Enabled()
		now := s.now()
		expires := deadline
		if ok && e.row.ExpiresAt != 0 {
			at := time.Unix(0, e.row.ExpiresAt)
			if expires.IsZero() || at.Before(expires) {
				expires = at
			}
		}
		s.mu.Unlock()
		if enabled && (!valid || !expires.IsZero() && !now.Before(expires)) {
			cancel()
			return
		}
		var timer *time.Timer
		var timeout <-chan time.Time
		if enabled && !expires.IsZero() {
			timer = time.NewTimer(expires.Sub(now))
			timeout = timer.C
		}
		select {
		case <-ctx.Done():
			if timer != nil {
				timer.Stop()
			}
			return
		case <-changed:
		case <-timeout:
		}
		if timer != nil {
			timer.Stop()
		}
	}
}

func (s *Service) Session(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if !s.sameOrigin(r) {
		Error(w, 403, "origin_forbidden", "Cross-origin access is not allowed.")
		return
	}
	switch r.Method {
	case http.MethodGet:
		_, code, _ := s.requestIdentity(r, true)
		if s.Enabled() && code != "" {
			unauthorized(w, code)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]bool{"enabled": s.Enabled(), "authenticated": code == ""}})
	case http.MethodPost:
		var body struct {
			Token string `json:"token"`
		}
		if json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&body) != nil {
			Error(w, 400, "invalid_request", "Provide a token.")
			return
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		id, code := s.validateLocked(body.Token)
		if code != "" {
			unauthorized(w, code)
			return
		}
		now := s.now()
		for key, sess := range s.sessions {
			if !now.Before(sess.expires) {
				delete(s.sessions, key)
			}
		}
		if len(s.sessions) >= 4096 {
			Error(w, 429, "session_limit", "Too many browser sessions. Try again later.")
			return
		}
		key := randomString(32)
		expiry := now.Add(12 * time.Hour)
		e := s.entries[id]
		if e.row.ExpiresAt != 0 && time.Unix(0, e.row.ExpiresAt).Before(expiry) {
			expiry = time.Unix(0, e.row.ExpiresAt)
		}
		s.sessions[key] = session{tokenID: id, expires: expiry}
		http.SetCookie(w, &http.Cookie{Name: CookieName, Value: key, Path: "/", HttpOnly: true, Secure: s.secureCookie(r), SameSite: http.SameSiteStrictMode, Expires: expiry})
		w.WriteHeader(204)
	case http.MethodDelete:
		if c, err := r.Cookie(CookieName); err == nil {
			s.mu.Lock()
			delete(s.sessions, c.Value)
			s.mu.Unlock()
		}
		http.SetCookie(w, &http.Cookie{Name: CookieName, Value: "", Path: "/", HttpOnly: true, Secure: s.secureCookie(r), SameSite: http.SameSiteStrictMode, MaxAge: -1})
		w.WriteHeader(204)
	default:
		w.Header().Set("Allow", "GET, POST, DELETE")
		w.WriteHeader(405)
	}
}

func (s *Service) UnaryInterceptor() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		if s.Enabled() {
			md, _ := metadata.FromIncomingContext(ctx)
			values := md.Get("authorization")
			if len(values) == 0 {
				return nil, status.Error(codes.Unauthenticated, "token_required")
			}
			if len(values) != 1 || !strings.HasPrefix(values[0], "Bearer ") {
				return nil, status.Error(codes.Unauthenticated, "token_invalid")
			}
			_, code := s.Validate(strings.TrimPrefix(values[0], "Bearer "))
			if code != "" {
				return nil, status.Error(codes.Unauthenticated, code)
			}
		}
		return handler(ctx, req)
	}
}

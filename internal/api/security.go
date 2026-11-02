package api

import (
	"encoding/json"
	"net/http"
	"sort"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/zfogg/spaniel/internal/security"
)

type SecurityStatus struct {
	Enabled               bool   `json:"enabled"`
	EncryptionPasswordSet bool   `json:"encryption_password_set"`
	EnabledSource         string `json:"enabled_source"`
	PasswordSource        string `json:"password_source"`
}

func (r *Router) securityService(w http.ResponseWriter) *security.Service {
	if r.settings == nil || r.settings.Security == nil {
		security.Error(w, 503, "security_unavailable", "Security service is unavailable.")
		return nil
	}
	w.Header().Set("Cache-Control", "no-store")
	return r.settings.Security
}
func (r *Router) securityStatus() SecurityStatus {
	s := r.settings
	return SecurityStatus{Enabled: s.Security.Enabled(), EncryptionPasswordSet: true, EnabledSource: s.SecurityEnabledSource, PasswordSource: s.SecurityPasswordSource}
}
func (r *Router) getSecurity(w http.ResponseWriter, req *http.Request) {
	if r.securityService(w) == nil {
		return
	}
	respond(w, r.securityStatus(), 1, 1)
}
func (r *Router) putSecurity(w http.ResponseWriter, req *http.Request) {
	svc := r.securityService(w)
	if svc == nil {
		return
	}
	var body struct {
		Enabled            *bool   `json:"enabled"`
		EncryptionPassword *string `json:"encryption_password"`
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, req.Body, 8192))
	dec.DisallowUnknownFields()
	if dec.Decode(&body) != nil {
		security.Error(w, 400, "invalid_request", "Invalid security settings.")
		return
	}
	s := r.settings
	s.mu.Lock()
	defer s.mu.Unlock()
	if body.Enabled != nil && s.SecurityEnabledSource != "config" || body.EncryptionPassword != nil && s.SecurityPasswordSource != "config" {
		security.Error(w, 409, "setting_overridden", "This setting is controlled by a flag or environment variable. Change that override and restart Spaniel.")
		return
	}
	password := ""
	if body.EncryptionPassword != nil {
		password = *body.EncryptionPassword
		if len(password) < 16 || len(password) > 1024 || strings.ContainsAny(password, "\x00\r\n") {
			security.Error(w, 400, "invalid_password", "Use an encryption password of 16 to 1024 bytes without NUL or line breaks.")
			return
		}
	}
	enabled := svc.Enabled()
	if body.Enabled != nil {
		enabled = *body.Enabled
	}
	if enabled && !svc.Enabled() && !svc.Authenticated(req) {
		security.Error(w, 409, "session_required", "Authenticate with a usable token before enabling security. Creating a token in this browser does this automatically.")
		return
	}
	oldEnabled, oldPassword := s.Viper.GetBool("api_token_security"), s.Viper.GetString("encryption_password")
	err := svc.Configure(enabled, password, func() error {
		s.Viper.Set("api_token_security", enabled)
		if password != "" {
			s.Viper.Set("encryption_password", password)
		}
		if err := s.Save(); err != nil {
			s.Viper.Set("api_token_security", oldEnabled)
			s.Viper.Set("encryption_password", oldPassword)
			return err
		}
		return nil
	})
	if err != nil {
		security.Error(w, 409, "security_update_failed", err.Error())
		return
	}
	respond(w, r.securityStatus(), 1, 1)
}
func (r *Router) listTokens(w http.ResponseWriter, req *http.Request) {
	svc := r.securityService(w)
	if svc == nil {
		return
	}
	tokens := svc.List()
	sort.Slice(tokens, func(i, j int) bool { return tokens[i].CreatedAt.After(tokens[j].CreatedAt) })
	respond(w, tokens, len(tokens), 1)
}
func (r *Router) createToken(w http.ResponseWriter, req *http.Request) {
	svc := r.securityService(w)
	if svc == nil {
		return
	}
	var body security.CreateInput
	dec := json.NewDecoder(http.MaxBytesReader(w, req.Body, 4096))
	dec.DisallowUnknownFields()
	if dec.Decode(&body) != nil {
		security.Error(w, 400, "invalid_request", "Invalid token request.")
		return
	}
	created, err := svc.Create(req.Context(), body)
	if err != nil {
		security.Error(w, 400, "token_creation_failed", err.Error())
		return
	}
	respond(w, created, 1, 1)
}
func (r *Router) deleteToken(w http.ResponseWriter, req *http.Request) {
	svc := r.securityService(w)
	if svc == nil {
		return
	}
	if err := svc.Delete(req.Context(), chi.URLParam(req, "tokenId")); err != nil {
		security.Error(w, 409, "token_deletion_failed", err.Error())
		return
	}
	respond(w, map[string]bool{"ok": true}, 1, 1)
}
func (r *Router) getSecuritySession(w http.ResponseWriter, req *http.Request) {
	r.securitySession(w, req)
}
func (r *Router) createSecuritySession(w http.ResponseWriter, req *http.Request) {
	r.securitySession(w, req)
}
func (r *Router) deleteSecuritySession(w http.ResponseWriter, req *http.Request) {
	r.securitySession(w, req)
}
func (r *Router) securitySession(w http.ResponseWriter, req *http.Request) {
	svc := r.securityService(w)
	if svc != nil {
		svc.Session(w, req)
	}
}

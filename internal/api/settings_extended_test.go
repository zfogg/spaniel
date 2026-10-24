package api

import (
	"bytes"
	"github.com/spf13/viper"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestExtendedSettingsSecretsAndValidation(t *testing.T) {
	router, svc, _ := newSettingsRouter(t)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodPut, "/api/settings", bytes.NewBufferString(`{"forward_bearer_token":"secret-never-return","ingestion_queue_requests":12,"otlp_http_url":"https://otlp.example.com:443"}`)))
	if w.Code != 200 {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	if bytes.Contains(w.Body.Bytes(), []byte("secret-never-return")) {
		t.Fatal("secret exposed")
	}
	if !bytes.Contains(w.Body.Bytes(), []byte(`"forward_bearer_token_set":true`)) {
		t.Fatal("missing configured flag")
	}
	persisted := viper.New()
	persisted.SetConfigFile(svc.ConfigPath)
	if e := persisted.ReadInConfig(); e != nil {
		t.Fatal(e)
	}
	if persisted.GetString("forward_bearer_token") != "secret-never-return" {
		t.Fatal("secret not persisted")
	}
	for _, body := range []string{`{"ingestion_queue_requests":0}`, `{"notification_timezone":"not/a/zone"}`, `{"notification_quiet_start":"25:00"}`, `{"otlp_grpc_url":"https://user:secret@example.com"}`, `{"forward_tls_key":"bad"}`} {
		w = httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest(http.MethodPut, "/api/settings", bytes.NewBufferString(body)))
		if w.Code != 400 {
			t.Fatalf("accepted invalid update: %s", body)
		}
	}
}
func TestQuietHoursOvernightTimezone(t *testing.T) {
	previous := notifications.Load()
	defer notifications.Store(previous)
	v := viper.New()
	v.Set("notification_quiet_enabled", true)
	v.Set("notification_quiet_start", "22:00")
	v.Set("notification_quiet_end", "08:00")
	v.Set("notification_timezone", "America/New_York")
	ConfigureNotificationPolicy(v)
	for _, tc := range []struct {
		stamp string
		quiet bool
	}{{"2026-10-23T03:00:00Z", true}, {"2026-10-23T16:00:00Z", false}} {
		now, _ := time.Parse(time.RFC3339, tc.stamp)
		if notificationQuiet(now) != tc.quiet {
			t.Fatal("incorrect quiet policy")
		}
	}
}

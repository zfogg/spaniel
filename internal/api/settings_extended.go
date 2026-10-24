package api

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/url"
	"strings"
	"time"
)

var ExtendedKeys = []string{"otlp_http_url", "otlp_grpc_url", "default_time_range", "refresh_interval_seconds", "ingestion_excluded_services", "ingestion_excluded_routes", "ingestion_max_request_mb", "ingestion_queue_requests", "ingestion_queue_mb", "traces_retention_enabled", "traces_retention_days", "logs_retention_enabled", "logs_retention_days", "metrics_retention_enabled", "metrics_retention_days", "notification_quiet_enabled", "notification_quiet_start", "notification_quiet_end", "notification_timezone", "notification_cooldown_seconds", "forward_bearer_token", "forward_tls_ca", "forward_tls_cert", "forward_tls_key"}

func validateExtended(u *SettingsUpdate) error {
	for _, p := range []*string{u.OtlpHttpUrl, u.OtlpGrpcUrl} {
		if p != nil && *p != "" {
			v, e := url.Parse(*p)
			if e != nil || v.Hostname() == "" || (v.Scheme != "http" && v.Scheme != "https") || v.User != nil || v.RawQuery != "" || v.Fragment != "" {
				return fmt.Errorf("OTLP endpoint must be an HTTP or HTTPS URL without credentials, query or fragment")
			}
		}
	}
	if u.DefaultTimeRange != nil {
		switch *u.DefaultTimeRange {
		case "15m", "1h", "24h", "all":
		default:
			return fmt.Errorf("invalid default time range")
		}
	}
	for _, p := range []*int{u.RefreshIntervalSeconds, u.NotificationCooldownSeconds} {
		if p != nil && (*p < 0 || *p > 86400) {
			return fmt.Errorf("interval must be 0–86400 seconds")
		}
	}
	for _, p := range []*int{u.IngestionMaxRequestMb, u.IngestionQueueMb} {
		if p != nil && (*p < 1 || *p > 1024) {
			return fmt.Errorf("ingestion memory limits must be 1–1024 MB")
		}
	}
	if u.IngestionQueueRequests != nil && (*u.IngestionQueueRequests < 1 || *u.IngestionQueueRequests > 10000) {
		return fmt.Errorf("queue requests must be 1–10000")
	}
	for _, p := range []*int{u.TracesRetentionDays, u.LogsRetentionDays, u.MetricsRetentionDays} {
		if p != nil && (*p < 1 || *p > 3650) {
			return fmt.Errorf("signal retention must be 1–3650 days")
		}
	}
	for _, p := range []*string{u.NotificationQuietStart, u.NotificationQuietEnd} {
		if p != nil {
			if _, e := time.Parse("15:04", *p); e != nil {
				return fmt.Errorf("quiet hours must use HH:MM")
			}
		}
	}
	if u.NotificationTimezone != nil {
		if _, e := time.LoadLocation(*u.NotificationTimezone); e != nil {
			return fmt.Errorf("invalid notification timezone")
		}
	}
	if u.ForwardBearerToken != nil && strings.ContainsAny(*u.ForwardBearerToken, "\x00\r\n") {
		return fmt.Errorf("invalid forwarding bearer token")
	}
	if u.ForwardTlsCa != nil && *u.ForwardTlsCa != "" && !x509.NewCertPool().AppendCertsFromPEM([]byte(*u.ForwardTlsCa)) {
		return fmt.Errorf("invalid forwarding CA PEM")
	}
	if u.ForwardTlsCert != nil || u.ForwardTlsKey != nil {
		if u.ForwardTlsCert == nil || u.ForwardTlsKey == nil {
			return fmt.Errorf("submit forwarding certificate and key together")
		}
		if *u.ForwardTlsCert != "" || *u.ForwardTlsKey != "" {
			if _, e := tls.X509KeyPair([]byte(*u.ForwardTlsCert), []byte(*u.ForwardTlsKey)); e != nil {
				return fmt.Errorf("invalid forwarding client certificate/key pair")
			}
		}
	}
	return nil
}

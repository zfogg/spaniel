package forwarder

import (
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"github.com/spf13/viper"
	"github.com/zfogg/spaniel/internal/telemetry"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"net/http"
	"strings"
)

type securityConfig struct {
	fingerprint [32]byte
	raw         *http.Transport
	token       string
	transport   http.RoundTripper
}
type secureTransport struct {
	f        *Forwarder
	fallback http.RoundTripper
}

func (s secureTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	c := s.f.security.Load()
	if c == nil {
		return s.fallback.RoundTrip(r)
	}
	copy := r.Clone(r.Context())
	copy.Header = r.Header.Clone()
	if c.token != "" {
		copy.Header.Set("Authorization", "Bearer "+c.token)
	}
	return c.transport.RoundTrip(copy)
}
func (f *Forwarder) ConfigureSecurity(v *viper.Viper) error {
	if f == nil {
		return nil
	}
	fingerprint := sha256.Sum256([]byte(strings.Join([]string{v.GetString("forward_bearer_token"), v.GetString("forward_tls_ca"), v.GetString("forward_tls_cert"), v.GetString("forward_tls_key")}, "\x00")))
	if current := f.security.Load(); current != nil && current.fingerprint == fingerprint {
		return nil
	}
	tc := &tls.Config{MinVersion: tls.VersionTLS12}
	if ca := v.GetString("forward_tls_ca"); ca != "" {
		pool, e := x509.SystemCertPool()
		if e != nil {
			pool = x509.NewCertPool()
		}
		if !pool.AppendCertsFromPEM([]byte(ca)) {
			return fmt.Errorf("invalid forwarding CA")
		}
		tc.RootCAs = pool
	}
	cert, key := v.GetString("forward_tls_cert"), v.GetString("forward_tls_key")
	if cert != "" || key != "" {
		pair, e := tls.X509KeyPair([]byte(cert), []byte(key))
		if e != nil {
			return fmt.Errorf("invalid forwarding client certificate/key")
		}
		tc.Certificates = []tls.Certificate{pair}
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = tc
	old := f.security.Swap(&securityConfig{fingerprint: fingerprint, raw: transport, token: v.GetString("forward_bearer_token"), transport: otelhttp.NewTransport(transport, otelhttp.WithSpanNameFormatter(telemetry.HTTP.ClientSpanName))})
	if old != nil {
		old.raw.CloseIdleConnections()
	}
	return nil
}

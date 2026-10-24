package forwarder

import (
	"context"
	"encoding/pem"
	"github.com/spf13/viper"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestForwardingSecurityTrustedCAAndToken(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer private-token" {
			t.Error("missing forwarding token")
		}
		w.WriteHeader(200)
	}))
	defer server.Close()
	f := New([]string{server.URL}, 1)
	defer f.Close()
	v := viper.New()
	v.Set("forward_bearer_token", "private-token")
	v.Set("forward_tls_ca", string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})))
	if e := f.ConfigureSecurity(v); e != nil {
		t.Fatal(e)
	}
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, server.URL, nil)
	resp, e := f.client.Do(req)
	if e != nil {
		t.Fatal(e)
	}
	resp.Body.Close()
	v.Set("forward_bearer_token", "")
	v.Set("forward_tls_ca", "bad")
	if f.ConfigureSecurity(v) == nil {
		t.Fatal("accepted invalid CA")
	}
}

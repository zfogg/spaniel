package forwarder

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestHTTPForwarderUsesSharedDestinationName(t *testing.T) {
	t.Setenv("OTEL_SDK_DISABLED", "false")
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder), sdktrace.WithSampler(sdktrace.AlwaysSample()))
	previous := otel.GetTracerProvider()
	otel.SetTracerProvider(provider)
	t.Cleanup(func() { otel.SetTracerProvider(previous); _ = provider.Shutdown(context.Background()) })
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if r.RequestURI != "/v1/traces?cursor=secret" || string(body) != "payload" {
			t.Error("HTTP destination or payload changed")
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	f := New(nil, 1)
	defer f.Close()
	response, err := f.client.Post(server.URL+"/v1/traces?cursor=secret", "application/json", strings.NewReader("payload"))
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	spans := recorder.Ended()
	if len(spans) != 1 {
		t.Fatalf("got %d spans", len(spans))
	}
	if got, want := spans[0].Name(), "spaniel.http.client.POST "+strings.TrimPrefix(server.URL, "http://")+"/v1/traces"; got != want {
		t.Fatalf("span name = %q, want %q", got, want)
	}
}

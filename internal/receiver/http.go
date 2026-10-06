package receiver

import (
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"go.opentelemetry.io/collector/pdata/plog/plogotlp"
	"go.opentelemetry.io/collector/pdata/pmetric/pmetricotlp"
	"go.opentelemetry.io/collector/pdata/ptrace/ptraceotlp"

	"github.com/zfogg/spaniel/internal/forwarder"
	"github.com/zfogg/spaniel/internal/ingestion"
	"github.com/zfogg/spaniel/internal/storage"
	"github.com/zfogg/spaniel/internal/telemetry"
)

// writeIngestError maps an ingest failure to an OTLP/HTTP status: a full
// database is retryable (503), everything else is a 500.
func writeIngestError(w http.ResponseWriter, err error) {
	if storage.IsStorageFull(err) {
		http.Error(w, "storage full: "+err.Error(), http.StatusServiceUnavailable)
		return
	}
	http.Error(w, "ingest: "+err.Error(), http.StatusInternalServerError)
}

// maxOTLPBodyBytes caps a single OTLP/HTTP export request body, matching the
// 64 MB ceiling the JSON import endpoint uses. Without it, io.ReadAll would
// buffer an unbounded payload and a single oversized (or malicious) request
// could exhaust process memory.
const maxOTLPBodyBytes = 64 << 20

// readBody reads the request body with a hard size cap. On failure it writes
// the response (413 when the cap is exceeded, 400 for any other read error)
// and reports ok=false so the caller can return immediately.
func readBody(w http.ResponseWriter, r *http.Request) (body []byte, ok bool) {
	r.Body = http.MaxBytesReader(w, r.Body, maxOTLPBodyBytes)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
			http.Error(w, "request body exceeds 64 MB limit", http.StatusRequestEntityTooLarge)
		} else {
			http.Error(w, "read body", http.StatusBadRequest)
		}
		return nil, false
	}
	return body, true
}

type HTTPReceiver struct {
	pipeline  *ingestion.Pipeline
	forwarder *forwarder.Forwarder // nil when no upstream configured
}

func NewHTTPReceiver(pipeline *ingestion.Pipeline) *HTTPReceiver {
	return &HTTPReceiver{pipeline: pipeline}
}

func (h *HTTPReceiver) SetForwarder(f *forwarder.Forwarder) {
	h.forwarder = f
}

func (h *HTTPReceiver) HandleTraces(w http.ResponseWriter, r *http.Request) {
	started, result, payloadBytes := time.Now(), "rejected", int64(0)
	defer func() {
		telemetry.Catalog().RecordReceive(r.Context(), "traces", result, payloadBytes, float64(time.Since(started).Microseconds())/1000)
	}()
	body, ok := readBody(w, r)
	if !ok {
		logOTLPHTTPFailure(r, "traces", "request body rejected", nil)
		return
	}
	payloadBytes = int64(len(body))
	req := ptraceotlp.NewExportRequest()
	var err error
	if isJSON(r.Header.Get("Content-Type")) {
		err = req.UnmarshalJSON(body)
	} else {
		err = req.UnmarshalProto(body)
	}
	if err != nil {
		logOTLPHTTPFailure(r, "traces", "request decode failed", err)
		http.Error(w, "unmarshal traces: "+err.Error(), http.StatusBadRequest)
		return
	}
	if err := h.pipeline.IngestTraces(r.Context(), req.Traces()); err != nil {
		logOTLPHTTPFailure(r, "traces", "ingest failed", err)
		writeIngestError(w, err)
		return
	}
	logOTLPHTTPIngest(r, "traces", req.Traces().SpanCount())
	if h.forwarder != nil {
		h.forwarder.Forward("/v1/traces", r.Header.Get("Content-Type"), body)
	}
	writeOTLPResponse(w, ptraceotlp.NewExportResponse(), isJSON(r.Header.Get("Content-Type")))
	result = "accepted"
}

func (h *HTTPReceiver) HandleLogs(w http.ResponseWriter, r *http.Request) {
	started, result, payloadBytes := time.Now(), "rejected", int64(0)
	defer func() {
		telemetry.Catalog().RecordReceive(r.Context(), "logs", result, payloadBytes, float64(time.Since(started).Microseconds())/1000)
	}()
	body, ok := readBody(w, r)
	if !ok {
		logOTLPHTTPFailure(r, "logs", "request body rejected", nil)
		return
	}
	payloadBytes = int64(len(body))
	req := plogotlp.NewExportRequest()
	var err error
	if isJSON(r.Header.Get("Content-Type")) {
		err = req.UnmarshalJSON(body)
	} else {
		err = req.UnmarshalProto(body)
	}
	if err != nil {
		logOTLPHTTPFailure(r, "logs", "request decode failed", err)
		http.Error(w, "unmarshal logs: "+err.Error(), http.StatusBadRequest)
		return
	}
	if err := h.pipeline.IngestLogs(r.Context(), req.Logs()); err != nil {
		logOTLPHTTPFailure(r, "logs", "ingest failed", err)
		writeIngestError(w, err)
		return
	}
	logOTLPHTTPIngest(r, "logs", req.Logs().LogRecordCount())
	if h.forwarder != nil {
		h.forwarder.Forward("/v1/logs", r.Header.Get("Content-Type"), body)
	}
	writeOTLPResponse(w, plogotlp.NewExportResponse(), isJSON(r.Header.Get("Content-Type")))
	result = "accepted"
}

func (h *HTTPReceiver) HandleMetrics(w http.ResponseWriter, r *http.Request) {
	started, result, payloadBytes := time.Now(), "rejected", int64(0)
	defer func() {
		telemetry.Catalog().RecordReceive(r.Context(), "metrics", result, payloadBytes, float64(time.Since(started).Microseconds())/1000)
	}()
	body, ok := readBody(w, r)
	if !ok {
		logOTLPHTTPFailure(r, "metrics", "request body rejected", nil)
		return
	}
	payloadBytes = int64(len(body))
	req := pmetricotlp.NewExportRequest()
	var err error
	if isJSON(r.Header.Get("Content-Type")) {
		err = req.UnmarshalJSON(body)
	} else {
		err = req.UnmarshalProto(body)
	}
	if err != nil {
		logOTLPHTTPFailure(r, "metrics", "request decode failed", err)
		http.Error(w, "unmarshal metrics: "+err.Error(), http.StatusBadRequest)
		return
	}
	if err := h.pipeline.IngestMetrics(r.Context(), req.Metrics()); err != nil {
		logOTLPHTTPFailure(r, "metrics", "ingest failed", err)
		writeIngestError(w, err)
		return
	}
	logOTLPHTTPIngest(r, "metrics", req.Metrics().DataPointCount())
	if h.forwarder != nil {
		h.forwarder.Forward("/v1/metrics", r.Header.Get("Content-Type"), body)
	}
	writeOTLPResponse(w, pmetricotlp.NewExportResponse(), isJSON(r.Header.Get("Content-Type")))
	result = "accepted"
}

func isJSON(ct string) bool {
	return strings.Contains(ct, "application/json")
}

// logOTLPHTTPIngest is the HTTP counterpart to logOTLPIngest. The explicit
// header guard preserves the same no-feedback-loop guarantee for a manually
// configured HTTP self-telemetry endpoint.
func logOTLPHTTPIngest(r *http.Request, signal string, count int) {
	if r.Header.Get("x-spaniel-self-telemetry") == "true" {
		return
	}
	slog.InfoContext(r.Context(), "OTLP telemetry ingested", "otel.signal", signal, "otel.record_count", count)
}

func logOTLPHTTPFailure(r *http.Request, signal, message string, err error) {
	if r.Header.Get("x-spaniel-self-telemetry") == "true" {
		return
	}
	args := []any{"otel.signal", signal}
	if err != nil {
		args = append(args, "error", err)
	}
	slog.ErrorContext(r.Context(), message, args...)
}

type otlpMarshaler interface {
	MarshalProto() ([]byte, error)
	MarshalJSON() ([]byte, error)
}

func writeOTLPResponse(w http.ResponseWriter, resp otlpMarshaler, json bool) {
	if json {
		data, _ := resp.MarshalJSON()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write(data) //nolint:errcheck
	} else {
		data, _ := resp.MarshalProto()
		w.Header().Set("Content-Type", "application/x-protobuf")
		w.WriteHeader(http.StatusOK)
		w.Write(data) //nolint:errcheck
	}
}

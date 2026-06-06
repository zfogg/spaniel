package receiver

import (
	"errors"
	"io"
	"net/http"
	"strings"

	"go.opentelemetry.io/collector/pdata/plog/plogotlp"
	"go.opentelemetry.io/collector/pdata/pmetric/pmetricotlp"
	"go.opentelemetry.io/collector/pdata/ptrace/ptraceotlp"

	"github.com/zfogg/spaniel/internal/forwarder"
	"github.com/zfogg/spaniel/internal/ingestion"
	"github.com/zfogg/spaniel/internal/storage"
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
	body, ok := readBody(w, r)
	if !ok {
		return
	}
	req := ptraceotlp.NewExportRequest()
	var err error
	if isJSON(r.Header.Get("Content-Type")) {
		err = req.UnmarshalJSON(body)
	} else {
		err = req.UnmarshalProto(body)
	}
	if err != nil {
		http.Error(w, "unmarshal traces: "+err.Error(), http.StatusBadRequest)
		return
	}
	if err := h.pipeline.IngestTraces(r.Context(), req.Traces()); err != nil {
		writeIngestError(w, err)
		return
	}
	if h.forwarder != nil {
		h.forwarder.Forward("/v1/traces", r.Header.Get("Content-Type"), body)
	}
	writeOTLPResponse(w, ptraceotlp.NewExportResponse(), isJSON(r.Header.Get("Content-Type")))
}

func (h *HTTPReceiver) HandleLogs(w http.ResponseWriter, r *http.Request) {
	body, ok := readBody(w, r)
	if !ok {
		return
	}
	req := plogotlp.NewExportRequest()
	var err error
	if isJSON(r.Header.Get("Content-Type")) {
		err = req.UnmarshalJSON(body)
	} else {
		err = req.UnmarshalProto(body)
	}
	if err != nil {
		http.Error(w, "unmarshal logs: "+err.Error(), http.StatusBadRequest)
		return
	}
	if err := h.pipeline.IngestLogs(r.Context(), req.Logs()); err != nil {
		writeIngestError(w, err)
		return
	}
	if h.forwarder != nil {
		h.forwarder.Forward("/v1/logs", r.Header.Get("Content-Type"), body)
	}
	writeOTLPResponse(w, plogotlp.NewExportResponse(), isJSON(r.Header.Get("Content-Type")))
}

func (h *HTTPReceiver) HandleMetrics(w http.ResponseWriter, r *http.Request) {
	body, ok := readBody(w, r)
	if !ok {
		return
	}
	req := pmetricotlp.NewExportRequest()
	var err error
	if isJSON(r.Header.Get("Content-Type")) {
		err = req.UnmarshalJSON(body)
	} else {
		err = req.UnmarshalProto(body)
	}
	if err != nil {
		http.Error(w, "unmarshal metrics: "+err.Error(), http.StatusBadRequest)
		return
	}
	if err := h.pipeline.IngestMetrics(r.Context(), req.Metrics()); err != nil {
		writeIngestError(w, err)
		return
	}
	if h.forwarder != nil {
		h.forwarder.Forward("/v1/metrics", r.Header.Get("Content-Type"), body)
	}
	writeOTLPResponse(w, pmetricotlp.NewExportResponse(), isJSON(r.Header.Get("Content-Type")))
}

func isJSON(ct string) bool {
	return strings.Contains(ct, "application/json")
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

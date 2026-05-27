package receiver

import (
	"context"
	"io"
	"net/http"
	"strings"

	"go.opentelemetry.io/collector/pdata/plog/plogotlp"
	"go.opentelemetry.io/collector/pdata/pmetric/pmetricotlp"
	"go.opentelemetry.io/collector/pdata/ptrace/ptraceotlp"

	"github.com/zfogg/spaniel/internal/forwarder"
	"github.com/zfogg/spaniel/internal/ingestion"
)

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
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "read body", http.StatusBadRequest)
		return
	}
	req := ptraceotlp.NewExportRequest()
	if isJSON(r.Header.Get("Content-Type")) {
		err = req.UnmarshalJSON(body)
	} else {
		err = req.UnmarshalProto(body)
	}
	if err != nil {
		http.Error(w, "unmarshal traces: "+err.Error(), http.StatusBadRequest)
		return
	}
	if err := h.pipeline.IngestTraces(context.Background(), req.Traces()); err != nil {
		http.Error(w, "ingest: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if h.forwarder != nil {
		h.forwarder.Forward("/v1/traces", r.Header.Get("Content-Type"), body)
	}
	writeOTLPResponse(w, ptraceotlp.NewExportResponse(), isJSON(r.Header.Get("Content-Type")))
}

func (h *HTTPReceiver) HandleLogs(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "read body", http.StatusBadRequest)
		return
	}
	req := plogotlp.NewExportRequest()
	if isJSON(r.Header.Get("Content-Type")) {
		err = req.UnmarshalJSON(body)
	} else {
		err = req.UnmarshalProto(body)
	}
	if err != nil {
		http.Error(w, "unmarshal logs: "+err.Error(), http.StatusBadRequest)
		return
	}
	if err := h.pipeline.IngestLogs(context.Background(), req.Logs()); err != nil {
		http.Error(w, "ingest: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if h.forwarder != nil {
		h.forwarder.Forward("/v1/logs", r.Header.Get("Content-Type"), body)
	}
	writeOTLPResponse(w, plogotlp.NewExportResponse(), isJSON(r.Header.Get("Content-Type")))
}

func (h *HTTPReceiver) HandleMetrics(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "read body", http.StatusBadRequest)
		return
	}
	req := pmetricotlp.NewExportRequest()
	if isJSON(r.Header.Get("Content-Type")) {
		err = req.UnmarshalJSON(body)
	} else {
		err = req.UnmarshalProto(body)
	}
	if err != nil {
		http.Error(w, "unmarshal metrics: "+err.Error(), http.StatusBadRequest)
		return
	}
	if err := h.pipeline.IngestMetrics(context.Background(), req.Metrics()); err != nil {
		http.Error(w, "ingest: "+err.Error(), http.StatusInternalServerError)
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

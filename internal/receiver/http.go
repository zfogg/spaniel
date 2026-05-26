package receiver

import (
	"context"
	"io"
	"net/http"
	"strings"

	"go.opentelemetry.io/collector/pdata/plog/plogotlp"
	"go.opentelemetry.io/collector/pdata/pmetric/pmetricotlp"
	"go.opentelemetry.io/collector/pdata/ptrace/ptraceotlp"

	"github.com/zfogg/spaniel/internal/ingestion"
)

type HTTPReceiver struct {
	pipeline *ingestion.Pipeline
}

func NewHTTPReceiver(pipeline *ingestion.Pipeline) *HTTPReceiver {
	return &HTTPReceiver{pipeline: pipeline}
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
	resp := ptraceotlp.NewExportResponse()
	data, _ := resp.MarshalProto()
	w.Header().Set("Content-Type", "application/x-protobuf")
	w.WriteHeader(http.StatusOK)
	w.Write(data) //nolint:errcheck
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
	resp := plogotlp.NewExportResponse()
	data, _ := resp.MarshalProto()
	w.Header().Set("Content-Type", "application/x-protobuf")
	w.WriteHeader(http.StatusOK)
	w.Write(data) //nolint:errcheck
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
	resp := pmetricotlp.NewExportResponse()
	data, _ := resp.MarshalProto()
	w.Header().Set("Content-Type", "application/x-protobuf")
	w.WriteHeader(http.StatusOK)
	w.Write(data) //nolint:errcheck
}

func isJSON(ct string) bool {
	return strings.Contains(ct, "application/json")
}

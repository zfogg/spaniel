package receiver

import (
	"context"
	"fmt"
	"net"

	"go.opentelemetry.io/collector/pdata/plog/plogotlp"
	"go.opentelemetry.io/collector/pdata/pmetric/pmetricotlp"
	"go.opentelemetry.io/collector/pdata/ptrace/ptraceotlp"
	"google.golang.org/grpc"

	"github.com/zfogg/spaniel/internal/ingestion"
)

type GRPCReceiver struct {
	pipeline *ingestion.Pipeline
	server   *grpc.Server
}

func NewGRPCReceiver(pipeline *ingestion.Pipeline) *GRPCReceiver {
	srv := grpc.NewServer()
	r := &GRPCReceiver{pipeline: pipeline, server: srv}
	ptraceotlp.RegisterGRPCServer(srv, &traceServer{pipeline: pipeline})
	plogotlp.RegisterGRPCServer(srv, &logServer{pipeline: pipeline})
	pmetricotlp.RegisterGRPCServer(srv, &metricServer{pipeline: pipeline})
	return r
}

func (r *GRPCReceiver) ListenAndServe(addr string) error {
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("grpc listen %s: %w", addr, err)
	}
	return r.server.Serve(lis)
}

// Serve runs the gRPC server on an already-bound listener. Safe to call from
// multiple goroutines with different listeners (e.g. one per IP family).
func (r *GRPCReceiver) Serve(lis net.Listener) error {
	return r.server.Serve(lis)
}

func (r *GRPCReceiver) Stop() {
	r.server.GracefulStop()
}

type traceServer struct {
	ptraceotlp.UnimplementedGRPCServer
	pipeline *ingestion.Pipeline
}

func (s *traceServer) Export(ctx context.Context, req ptraceotlp.ExportRequest) (ptraceotlp.ExportResponse, error) {
	if err := s.pipeline.IngestTraces(ctx, req.Traces()); err != nil {
		return ptraceotlp.NewExportResponse(), err
	}
	return ptraceotlp.NewExportResponse(), nil
}

type logServer struct {
	plogotlp.UnimplementedGRPCServer
	pipeline *ingestion.Pipeline
}

func (s *logServer) Export(ctx context.Context, req plogotlp.ExportRequest) (plogotlp.ExportResponse, error) {
	if err := s.pipeline.IngestLogs(ctx, req.Logs()); err != nil {
		return plogotlp.NewExportResponse(), err
	}
	return plogotlp.NewExportResponse(), nil
}

type metricServer struct {
	pmetricotlp.UnimplementedGRPCServer
	pipeline *ingestion.Pipeline
}

func (s *metricServer) Export(ctx context.Context, req pmetricotlp.ExportRequest) (pmetricotlp.ExportResponse, error) {
	if err := s.pipeline.IngestMetrics(ctx, req.Metrics()); err != nil {
		return pmetricotlp.NewExportResponse(), err
	}
	return pmetricotlp.NewExportResponse(), nil
}

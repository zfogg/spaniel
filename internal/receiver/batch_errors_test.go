package receiver

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/zfogg/spaniel/internal/ingestion"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestBatchAdmissionErrorsAreRetryable(t *testing.T) {
	for _, tc := range []struct {
		err  error
		code codes.Code
	}{{ingestion.ErrIngestBusy, codes.ResourceExhausted}, {ingestion.ErrIngestClosed, codes.Unavailable}} {
		w := httptest.NewRecorder()
		writeIngestError(w, tc.err)
		if w.Code != http.StatusServiceUnavailable {
			t.Fatalf("HTTP status=%d", w.Code)
		}
		if w.Header().Get("Retry-After") != "1" {
			t.Fatal("missing HTTP retry delay")
		}
		if tc.err == ingestion.ErrIngestBusy {
			details := status.Convert(ingestGRPCError(tc.err)).Details()
			if len(details) != 1 {
				t.Fatal("RESOURCE_EXHAUSTED must signal recoverability")
			}
			retry, ok := details[0].(*errdetails.RetryInfo)
			if !ok || retry.RetryDelay.GetSeconds() != 1 {
				t.Fatalf("retry info: %v", details)
			}
		}
		if code := status.Code(ingestGRPCError(tc.err)); code != tc.code {
			t.Fatalf("gRPC status=%v", code)
		}
	}
}

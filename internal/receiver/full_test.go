package receiver

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/zfogg/spaniel/internal/storage"
)

func TestWriteIngestError(t *testing.T) {
	// Storage full → 503 (retryable).
	w := httptest.NewRecorder()
	writeIngestError(w, storage.ErrStorageFull)
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("full: status = %d, want 503", w.Code)
	}
	// Anything else → 500.
	w2 := httptest.NewRecorder()
	writeIngestError(w2, errors.New("boom"))
	if w2.Code != http.StatusInternalServerError {
		t.Errorf("generic: status = %d, want 500", w2.Code)
	}
}

func TestIngestGRPCError(t *testing.T) {
	if c := status.Code(ingestGRPCError(storage.ErrStorageFull)); c != codes.ResourceExhausted {
		t.Errorf("full: code = %v, want ResourceExhausted", c)
	}
	// Non-full errors pass through (gRPC maps a plain error to Unknown).
	if c := status.Code(ingestGRPCError(errors.New("boom"))); c != codes.Unknown {
		t.Errorf("generic: code = %v, want Unknown", c)
	}
	if ingestGRPCError(nil) != nil {
		t.Error("nil error should map to nil")
	}
}

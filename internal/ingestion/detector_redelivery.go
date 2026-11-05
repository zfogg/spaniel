package ingestion

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/zfogg/spaniel/internal/ingestion/detectors"
	"github.com/zfogg/spaniel/internal/telemetry"
	"github.com/zfogg/spaniel/internal/ws"
	"gorm.io/gorm"
)

type redeliveryDetector struct {
	mu       sync.Mutex
	detector *detectors.RedeliveryDetector
}

func (p *Pipeline) runRedeliveryDetector(traceID, sessionID string) {
	spans, err := p.store.GetTraceInSession(traceID, sessionID)
	if err != nil {
		slog.Warn("redelivery detector read failed", "trace_id", traceID, "error", err)
		return
	}
	if !detectors.HasRedeliveryCandidates(spans) {
		return
	}
	p.redeliveries.mu.Lock()
	defer p.redeliveries.mu.Unlock()
	if p.redeliveries.detector == nil {
		key, err := p.store.RedeliveryKey()
		if err != nil {
			slog.Warn("redelivery key unavailable", "error", err)
			return
		}
		p.redeliveries.detector = detectors.NewRedeliveryDetector(key)
	}
	for _, issue := range p.redeliveries.detector.Analyze(spans, time.Now().UnixNano()) {
		existing, err := p.store.GetTraceIssue(issue.ID)
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			slog.Warn("redelivery finding lookup failed", "error", err)
			continue
		}
		if existing != nil {
			// Trace identity is immutable in the store. Preserve its span as well
			// when evidence is reconstructed after a restart or idle expiry.
			issue.TraceID, issue.ExampleSpanID, issue.ParentSpanID = existing.TraceID, existing.ExampleSpanID, existing.ParentSpanID
			if existing.Count > issue.Count {
				issue.Count, issue.Fingerprint = existing.Count, existing.Fingerprint
			}
		}
		if err := p.store.UpsertTraceIssue(issue); err != nil {
			slog.Warn("redelivery finding write failed", "error", err)
		} else {
			telemetry.Catalog().RecordDetectorIssue(context.Background(), issue.Kind)
			if p.hub != nil {
				p.hub.Broadcast(ws.NewIssueEvent(&ws.IssuePayload{TraceID: issue.TraceID, Kind: issue.Kind, Fingerprint: issue.Fingerprint, Count: issue.Count}))
			}
		}
	}
}

package ingestion

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"log/slog"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/zfogg/spaniel/internal/ingestion/detectors"
	"github.com/zfogg/spaniel/internal/storage"
)

const metricSeriesLimit = storage.MetricSeriesLimit

type metricEvaluationSlot struct {
	key uint64
	at  int64
}

// A fixed-size throttle bounds diagnostic work independently of stream count.
// Open addressing keeps hash collisions from permanently hiding busy streams.
func (p *Pipeline) scheduleMetricCardinality(ctx context.Context, session, service, name string) {
	h := fnv.New64a()
	_, _ = h.Write([]byte(session + "\x00" + service + "\x00" + name))
	key := h.Sum64()
	if key == 0 {
		key = 1
	}
	now := time.Now().UnixNano()
	p.metricEvaluationMu.Lock()
	available := -1
	for offset := range p.metricEvaluation {
		i := int((key + uint64(offset)) % uint64(len(p.metricEvaluation)))
		slot := p.metricEvaluation[i]
		if slot.key == key {
			if now >= slot.at && now-slot.at < int64(time.Minute) {
				p.metricEvaluationMu.Unlock()
				return
			}
			available = i
			break
		}
		if available < 0 && (slot.key == 0 || now < slot.at || now-slot.at >= int64(time.Minute)) {
			available = i
		}
	}
	if available < 0 {
		p.metricEvaluationMu.Unlock()
		return
	}
	p.metricEvaluation[available] = metricEvaluationSlot{key: key, at: now}
	p.metricEvaluationMu.Unlock()
	epoch := p.store.MetricCatalogEpoch()
	p.afterPersist(ctx, func() {
		if p.store.MetricCatalogEpoch() == epoch {
			p.evaluateMetricCardinality(ctx, session, service, name, now)
		}
	})
}

func (p *Pipeline) evaluateMetricCardinality(ctx context.Context, session, service, name string, now int64) {
	p.evaluateMetricCardinalityWithLimit(ctx, session, service, name, now, metricSeriesLimit)
}

func (p *Pipeline) evaluateMetricCardinalityWithLimit(ctx context.Context, session, service, name string, now int64, limit int) {
	store := p.store.WithContext(ctx)
	history, err := store.ObserveMetricAdmissions(session, service, name, now, limit)
	if err != nil {
		slog.Warn("metric cardinality history read failed", "error", err)
		return
	}
	if len(history.Series) > limit {
		return
	}
	e := detectors.CardinalityEvidence{Active: len(history.Series), Limit: limit, StartedAt: history.StartedAtNs, Now: now}
	keys := map[string]struct{}{}
	for _, row := range history.Series {
		if row.AdmittedAtNs != nil {
			t := *row.AdmittedAtNs
			if t >= now-int64(time.Hour) && t < now-int64(30*time.Minute) {
				e.OlderHalf++
			}
			if t >= now-int64(30*time.Minute) && t <= now {
				e.NewerHalf++
			}
		}
		var attrs map[string]json.RawMessage
		if json.Unmarshal([]byte(row.SeriesAttributes), &attrs) == nil {
			for key := range attrs {
				keys[key] = struct{}{}
			}
		}
	}
	result := detectors.EvaluateCardinality(e)
	if result.Status != "growth" && result.Status != "approaching_limit" {
		return
	}
	keyList := make([]string, 0, len(keys))
	for key := range keys {
		keyList = append(keyList, key)
	}
	sort.Strings(keyList)
	id := sha256.Sum256([]byte(fmt.Sprintf("%s\x00%s\x00%s\x00%s\x00%d", session, service, name, result.Status, history.StartedAtNs)))
	query := url.Values{"session": {session}, "service": {service}, "metric": {name}}
	err = store.RecordNotification(&storage.NotificationRecord{Source: "issue", SourceID: fmt.Sprintf("%x", id), Severity: "warning", Title: "Metric cardinality " + strings.ReplaceAll(result.Status, "_", " "), Body: fmt.Sprintf("%s / %s: %d series, %d admitted in the last hour (%d then %d); limit %d. Attribute keys: %s", service, name, e.Active, result.Growth, e.OlderHalf, e.NewerHalf, e.Limit, strings.Join(keyList, ", ")), Link: "/metrics?" + query.Encode(), DedupeKey: fmt.Sprintf("metric-cardinality:%x", id), CreatedAt: now})
	if err != nil {
		slog.Warn("metric cardinality finding write failed", "error", err)
	}
}

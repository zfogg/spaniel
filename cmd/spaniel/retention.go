package main

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"

	"github.com/zfogg/spaniel/internal/storage"
)

const (
	storageGuardInterval      = 5 * time.Second
	storageHighWaterPercent   = int64(80)
	storagePruneTargetPercent = int64(70)
	storageMaintenanceBudget  = 8 * time.Second
	storageWALCheckpointBytes = storage.ManagedWALCheckpointBytes
)

var (
	retentionDeletedCounterOnce sync.Once
	retentionDeletedCounter     metric.Int64Counter

	retentionDurationHistOnce sync.Once
	retentionDurationHist     metric.Float64Histogram
)

// storageGuardPolicy is updated live by the settings API and read by both
// background retention loops without racing on viper.
type storageGuardPolicy struct {
	maxBytes  atomic.Int64
	autoPrune atomic.Bool
}

func newStorageGuardPolicy(maxBytes int64, autoPrune bool) *storageGuardPolicy {
	p := &storageGuardPolicy{}
	p.Update(maxBytes, autoPrune)
	return p
}

func (p *storageGuardPolicy) Update(maxBytes int64, autoPrune bool) {
	p.maxBytes.Store(maxBytes)
	p.autoPrune.Store(autoPrune)
}

func (p *storageGuardPolicy) RetentionSizeLimit() int64 {
	if !p.autoPrune.Load() {
		return 0
	}
	return p.maxBytes.Load()
}

func getRetentionDeletedCounter() metric.Int64Counter {
	retentionDeletedCounterOnce.Do(func() {
		meter := otel.Meter("spaniel/retention")
		retentionDeletedCounter, _ = meter.Int64Counter("spaniel.retention.deleted_sessions",
			metric.WithDescription("Sessions deleted by the retention policy"),
			metric.WithUnit("{session}"),
		)
	})
	return retentionDeletedCounter
}

func getRetentionDurationHist() metric.Float64Histogram {
	retentionDurationHistOnce.Do(func() {
		meter := otel.Meter("spaniel/retention")
		retentionDurationHist, _ = meter.Float64Histogram("spaniel.retention.duration",
			metric.WithDescription("Time taken to run a retention policy pass"),
			metric.WithUnit("ms"),
		)
	})
	return retentionDurationHist
}

func retentionConfig(days, maxSessions, maxDBSizeMB int) storage.RetentionConfig {
	cfg := storage.RetentionConfig{
		MaxSessions: maxSessions,
	}
	if days > 0 {
		cfg.MaxAge = time.Duration(days) * 24 * time.Hour
	}
	if maxDBSizeMB > 0 {
		cfg.MaxDBSizeBytes = int64(maxDBSizeMB) * 1024 * 1024
	}
	return cfg
}

// runRetention applies age/count retention immediately and then hourly. The
// size limit is merged from the live policy so disabling auto-prune stops every
// automatic size-based deletion path without affecting explicit retention.
func runRetention(store *storage.DB, cfg storage.RetentionConfig, policy *storageGuardPolicy) {
	apply := func() {
		ctx, span := otel.Tracer("spaniel/retention").Start(context.Background(), "spaniel.retention.prune")
		t0 := time.Now()
		current := cfg
		current.MaxDBSizeBytes = policy.RetentionSizeLimit()
		current.MaxDuration = storageMaintenanceBudget
		scopedStore := store.WithContext(ctx)
		res, err := scopedStore.Prune(current, scopedStore.ActiveSessionID())
		getRetentionDurationHist().Record(ctx, float64(time.Since(t0).Milliseconds()))
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			span.End()
			slog.Error("retention prune failed", "err", err)
			return
		}
		deleted := res.DeletedByAge + res.DeletedByCount + res.DeletedBySize
		span.SetAttributes(
			attribute.Int("retention.deleted_by_age", res.DeletedByAge),
			attribute.Int("retention.deleted_by_count", res.DeletedByCount),
			attribute.Int("retention.deleted_by_size", res.DeletedBySize),
			attribute.Int("retention.final_sessions", res.FinalSessions),
			attribute.Int64("retention.final_db_bytes", res.FinalDBSizeBytes),
		)
		span.End()
		if deleted > 0 {
			getRetentionDeletedCounter().Add(ctx, int64(res.DeletedByAge),
				metric.WithAttributes(attribute.String("reason", "age")))
			getRetentionDeletedCounter().Add(ctx, int64(res.DeletedByCount),
				metric.WithAttributes(attribute.String("reason", "count")))
			getRetentionDeletedCounter().Add(ctx, int64(res.DeletedBySize),
				metric.WithAttributes(attribute.String("reason", "size")))
			slog.Info("retention pruned sessions",
				"deleted", deleted,
				"by_age", res.DeletedByAge,
				"by_count", res.DeletedByCount,
				"by_size", res.DeletedBySize,
				"remaining", res.FinalSessions,
				"db_mb", float64(res.FinalDBSizeBytes)/1024/1024,
			)
		}
	}
	apply()
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for range ticker.C {
		apply()
	}
}

type storageGuardStore interface {
	UsedSize() int64
	ActiveSessionID() string
	Prune(storage.RetentionConfig, string) (storage.PruneResult, error)
	SetFull(bool)
	RetentionBackingOff() bool
}

// checkStorageGuard applies one storage policy pass. With auto-prune disabled,
// it never deletes and marks storage full only at the hard cap. With it enabled,
// pruning begins at 80% and targets 70%, leaving headroom for ingest bursts. If
// pruning cannot restore that headroom, ingestion pauses before the hard cap.
func checkStorageGuard(store storageGuardStore, policy *storageGuardPolicy) {
	maxBytes := policy.maxBytes.Load()
	if maxBytes <= 0 {
		store.SetFull(false)
		return
	}

	size := store.UsedSize()
	if !policy.autoPrune.Load() {
		full := size >= maxBytes
		store.SetFull(full)
		if full {
			slog.Warn("storage full: auto-prune disabled; ingestion paused",
				"db_mb", float64(size)/1024/1024,
				"cap_mb", float64(maxBytes)/1024/1024)
		}
		return
	}

	highWater := maxBytes * storageHighWaterPercent / 100
	target := maxBytes * storagePruneTargetPercent / 100
	if highWater <= 0 {
		highWater = maxBytes
	}
	if target <= 0 || target >= highWater {
		target = highWater - 1
	}
	if size < highWater {
		store.SetFull(false)
		return
	}
	// Enforce the real cap before maintenance; failure to reach the proactive
	// target alone is not full.
	store.SetFull(size >= maxBytes)

	res, err := store.Prune(storage.RetentionConfig{MaxDBSizeBytes: target, MaxDuration: storageMaintenanceBudget}, store.ActiveSessionID())
	after := store.UsedSize()
	soft := storage.IsRetentionTargetUnreached(err)
	if err != nil && !soft {
		slog.Error("storage guard auto-prune failed", "err", err)
	}
	// Staying above the proactive high-water mark after one pass is not a
	// storage-full condition: queued ingestion may flush as appenders reopen,
	// and the next guard pass can prune again. Reject writes only when pruning
	// failed or the hard cap itself is still reached.
	blocked := (err != nil && !soft) || after >= maxBytes
	store.SetFull(blocked)
	if blocked {
		slog.Warn("storage guard could not restore headroom; ingestion paused",
			"db_mb", float64(after)/1024/1024,
			"high_water_mb", float64(highWater)/1024/1024,
			"cap_mb", float64(maxBytes)/1024/1024)
		return
	}
	deleted := res.DeletedByAge + res.DeletedByCount + res.DeletedBySize
	if deleted > 0 {
		slog.Info("storage guard auto-pruned oldest telemetry",
			"deleted", deleted,
			"db_mb", float64(after)/1024/1024,
			"target_mb", float64(target)/1024/1024,
			"cap_mb", float64(maxBytes)/1024/1024)
	}
}

// runStorageGuard enforces the live size policy between hourly retention
// passes. Checking every five seconds provides enough headroom for bursty
// local telemetry without continuously querying DuckDB.
func runStorageGuard(store *storage.DB, policy *storageGuardPolicy) {
	runStorageGuardChecks(context.Background(), storageGuardInterval, func() {
		if checkStorageWAL(store, policy, store.WALSize(), func(ctx context.Context) error {
			return store.WithContext(ctx).Checkpoint()
		}) {
			return
		}
		checkStorageGuard(store, policy)
	})
}

// Own WAL checkpoints instead of allowing a native transaction commit to start
// one while appenders/readers remain active. This also runs in preserve mode
// and with no size cap. At high water Prune already checkpoints; give eviction
// priority so sustained WAL traffic cannot starve oldest-first reclamation.
func checkStorageWAL(store storageGuardStore, policy *storageGuardPolicy, walBytes int64, checkpoint func(context.Context) error) bool {
	if walBytes < storageWALCheckpointBytes {
		return false
	}
	maxBytes := policy.maxBytes.Load()
	size := store.UsedSize()
	if maxBytes > 0 && policy.autoPrune.Load() && !store.RetentionBackingOff() && size >= maxBytes*storageHighWaterPercent/100 {
		return false
	}
	store.SetFull(maxBytes > 0 && size >= maxBytes)
	ctx, cancel := context.WithTimeout(context.Background(), storageMaintenanceBudget)
	defer cancel()
	err := checkpoint(ctx)
	if err != nil {
		slog.Warn("storage WAL checkpoint failed", "err", err)
	}
	// A budget expiry alone cannot declare a below-cap durable WAL full.
	failed := err != nil && !storage.IsMaintenanceTimeout(err)
	store.SetFull(failed || (maxBytes > 0 && store.UsedSize() >= maxBytes))
	return true
}

func runStorageGuardChecks(ctx context.Context, interval time.Duration, check func()) {
	for ctx.Err() == nil {
		check()
		// A ticker retains an overdue tick while a long rebuild runs, causing
		// back-to-back pauses. Start the next delay only after this pass ends.
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

// prune opens the DB and applies the retention policy once. The active session
// is unknown at CLI invocation time, so we pass "" — no session is special.
func prune(dbPath string, cfg storage.RetentionConfig) error {
	store, err := storage.Open(dbPath)
	if err != nil {
		return fmt.Errorf("open storage: %w", err)
	}
	defer store.Close()

	res, err := store.Prune(cfg, "")
	if err != nil {
		return err
	}
	fmt.Printf("pruned: age=%d count=%d size=%d → %d sessions remain, %.1f MB on disk\n",
		res.DeletedByAge, res.DeletedByCount, res.DeletedBySize,
		res.FinalSessions, float64(res.FinalDBSizeBytes)/1024/1024,
	)
	return nil
}

// reset wipes all data in the DB file.
func reset(dbPath string) error {
	store, err := storage.Open(dbPath)
	if err != nil {
		return fmt.Errorf("open storage: %w", err)
	}
	defer store.Close()
	if err := store.Reset(); err != nil {
		return err
	}
	fmt.Println("all spaniel data wiped")
	return nil
}

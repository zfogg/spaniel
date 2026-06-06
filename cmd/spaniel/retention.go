package main

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"

	"github.com/zfogg/spaniel/internal/storage"
)

var (
	retentionDeletedCounterOnce sync.Once
	retentionDeletedCounter     metric.Int64Counter

	retentionDurationHistOnce sync.Once
	retentionDurationHist     metric.Float64Histogram
)

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

// runRetention applies the retention policy immediately, then every hour for the
// lifetime of the process. Errors are logged but never fatal.
func runRetention(store *storage.DB, cfg storage.RetentionConfig, activeID string) {
	apply := func() {
		ctx, span := otel.Tracer("spaniel/retention").Start(context.Background(), "retention.prune")
		t0 := time.Now()
		res, err := store.Prune(cfg, activeID)
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

// runStorageGuard enforces the size cap between the hourly retention passes:
// every 30s, if the DB is at/over maxBytes it prunes by size (protecting the
// current active session), then flips storage full/not-full. Ingestion is
// rejected (503) only while genuinely over the cap and unable to free space.
func runStorageGuard(store *storage.DB, maxBytes int64) {
	if maxBytes <= 0 {
		return
	}
	check := func() {
		if store.FileSize() < maxBytes {
			store.SetFull(false)
			return
		}
		// Over the cap: shrink by deleting the oldest non-active, non-baseline
		// sessions. If nothing more can be freed, storage is full.
		if _, err := store.Prune(storage.RetentionConfig{MaxDBSizeBytes: maxBytes}, store.ActiveSessionID()); err != nil {
			slog.Error("storage guard prune failed", "err", err)
		}
		full := store.FileSize() >= maxBytes
		store.SetFull(full)
		if full {
			slog.Warn("storage full: ingestion paused until space frees",
				"db_mb", float64(store.FileSize())/1024/1024,
				"cap_mb", float64(maxBytes)/1024/1024)
		}
	}
	check()
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		check()
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

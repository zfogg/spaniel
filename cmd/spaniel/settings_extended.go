package main

import (
	"context"
	"github.com/spf13/viper"
	"github.com/zfogg/spaniel/internal/api"
	"github.com/zfogg/spaniel/internal/forwarder"
	"github.com/zfogg/spaniel/internal/ingestion"
	"github.com/zfogg/spaniel/internal/storage"
	"log/slog"
	"sync/atomic"
	"time"
)

type extendedRuntime struct {
	policy atomic.Pointer[storage.SignalRetention]
	cancel context.CancelFunc
	done   chan struct{}
}

func newExtendedRuntime(v *viper.Viper, p *ingestion.Pipeline, f *forwarder.Forwarder, db *storage.DB) (*extendedRuntime, error) {
	ctx, cancel := context.WithCancel(context.Background())
	r := &extendedRuntime{cancel: cancel, done: make(chan struct{})}
	if e := r.apply(v, p, f); e != nil {
		cancel()
		return nil, e
	}
	go func() {
		defer close(r.done)
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if policy := r.policy.Load(); policy != nil {
					if e := db.PruneSignals(*policy); e != nil {
						slog.Error("signal retention failed", "error", e)
					}
				}
			}
		}
	}()
	return r, nil
}
func (r *extendedRuntime) apply(v *viper.Viper, p *ingestion.Pipeline, f *forwarder.Forwarder) error {
	if e := f.ConfigureSecurity(v); e != nil {
		return e
	}
	p.Configure(v)
	api.ConfigureNotificationPolicy(v)
	policy := &storage.SignalRetention{}
	if v.GetBool("traces_retention_enabled") {
		policy.TracesDays = v.GetInt("traces_retention_days")
	}
	if v.GetBool("logs_retention_enabled") {
		policy.LogsDays = v.GetInt("logs_retention_days")
	}
	if v.GetBool("metrics_retention_enabled") {
		policy.MetricsDays = v.GetInt("metrics_retention_days")
	}
	r.policy.Store(policy)
	return nil
}
func (r *extendedRuntime) Close() { r.cancel(); <-r.done }

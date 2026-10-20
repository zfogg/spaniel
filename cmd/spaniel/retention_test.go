package main

import (
	"errors"
	"testing"

	"github.com/zfogg/spaniel/internal/storage"
)

type fakeStorageGuardStore struct {
	size         int64
	activeID     string
	full         bool
	pruneCalls   int
	pruneLimit   int64
	pruneErr     error
	pruneBatches int
	retainSize   bool
}

func (f *fakeStorageGuardStore) UsedSize() int64         { return f.size }
func (f *fakeStorageGuardStore) ActiveSessionID() string { return f.activeID }
func (f *fakeStorageGuardStore) SetFull(full bool)       { f.full = full }
func (f *fakeStorageGuardStore) Prune(cfg storage.RetentionConfig, activeID string) (storage.PruneResult, error) {
	f.pruneCalls++
	f.pruneLimit = cfg.MaxDBSizeBytes
	f.pruneBatches = cfg.MaxSizeBatches
	if f.pruneErr != nil {
		return storage.PruneResult{}, f.pruneErr
	}
	if !f.retainSize {
		f.size = cfg.MaxDBSizeBytes - 1
	}
	return storage.PruneResult{DeletedBySize: 2, FinalDBSizeBytes: f.size}, nil
}

func TestStorageGuardDisabledPreservesDataUntilCap(t *testing.T) {
	policy := newStorageGuardPolicy(100, false)
	store := &fakeStorageGuardStore{size: 99}

	_ = checkStorageGuard(store, policy)
	if store.full {
		t.Fatal("storage below the cap should remain writable")
	}
	if store.pruneCalls != 0 {
		t.Fatalf("auto-prune disabled: got %d prune calls, want 0", store.pruneCalls)
	}

	store.size = 100
	_ = checkStorageGuard(store, policy)
	if !store.full {
		t.Fatal("storage at the cap should report full")
	}
	if store.pruneCalls != 0 {
		t.Fatalf("auto-prune disabled at cap: got %d prune calls, want 0", store.pruneCalls)
	}
}

func TestStorageGuardEnabledPrunesAt95PercentTo65Percent(t *testing.T) {
	policy := newStorageGuardPolicy(100, true)
	store := &fakeStorageGuardStore{size: 94, activeID: "active"}
	_ = checkStorageGuard(store, policy)
	if store.pruneCalls != 0 || store.full {
		t.Fatal("storage below the high-water mark should retain telemetry and remain writable")
	}
	store.size = 95

	_ = checkStorageGuard(store, policy)

	if store.pruneCalls != 1 {
		t.Fatalf("got %d prune calls, want 1", store.pruneCalls)
	}
	if store.pruneLimit != 65 {
		t.Fatalf("prune target = %d, want 65", store.pruneLimit)
	}
	if store.full {
		t.Fatal("successful proactive prune should leave storage writable")
	}
}

func TestStorageGuardEnabledPausesBeforeCapWhenPruneFails(t *testing.T) {
	policy := newStorageGuardPolicy(100, true)
	store := &fakeStorageGuardStore{size: 95, pruneErr: errors.New("boom")}

	_ = checkStorageGuard(store, policy)

	if !store.full {
		t.Fatal("failed proactive prune should pause ingestion before the hard cap")
	}
}

func TestStorageGuardYieldsAfterOneBatchAboveHighWater(t *testing.T) {
	store := &fakeStorageGuardStore{size: 96, retainSize: true}
	if err := checkStorageGuard(store, newStorageGuardPolicy(100, true)); err != nil {
		t.Fatal(err)
	}
	if store.pruneCalls != 1 || store.pruneBatches != 1 || store.full {
		t.Fatalf("automatic pruning must yield and remain writable below cap: %+v", store)
	}
}

func TestStorageGuardReportsInvalidatedDatabase(t *testing.T) {
	policy := newStorageGuardPolicy(100, true)
	store := &fakeStorageGuardStore{
		size:     95,
		pruneErr: errors.New("database has been invalidated because of a previous fatal error"),
	}

	err := checkStorageGuard(store, policy)
	if !storage.IsDatabaseInvalidated(err) {
		t.Fatalf("checkStorageGuard error = %v, want invalidated database", err)
	}
}

func TestStoragePolicyDisablesAutomaticSizeRetention(t *testing.T) {
	policy := newStorageGuardPolicy(500, true)
	if got := policy.RetentionSizeLimit(); got != 500 {
		t.Fatalf("enabled retention size limit = %d, want 500", got)
	}
	policy.Update(500, false)
	if got := policy.RetentionSizeLimit(); got != 0 {
		t.Fatalf("disabled retention size limit = %d, want 0", got)
	}
}

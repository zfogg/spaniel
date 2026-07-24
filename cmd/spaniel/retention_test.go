package main

import (
	"errors"
	"testing"

	"github.com/zfogg/spaniel/internal/storage"
)

type fakeStorageGuardStore struct {
	size       int64
	activeID   string
	full       bool
	pruneCalls int
	pruneLimit int64
	pruneErr   error
}

func (f *fakeStorageGuardStore) UsedSize() int64         { return f.size }
func (f *fakeStorageGuardStore) ActiveSessionID() string { return f.activeID }
func (f *fakeStorageGuardStore) SetFull(full bool)       { f.full = full }
func (f *fakeStorageGuardStore) Prune(cfg storage.RetentionConfig, activeID string) (storage.PruneResult, error) {
	f.pruneCalls++
	f.pruneLimit = cfg.MaxDBSizeBytes
	if f.pruneErr != nil {
		return storage.PruneResult{}, f.pruneErr
	}
	f.size = cfg.MaxDBSizeBytes - 1
	return storage.PruneResult{DeletedBySize: 2, FinalDBSizeBytes: f.size}, nil
}

func TestStorageGuardDisabledPreservesDataUntilCap(t *testing.T) {
	policy := newStorageGuardPolicy(100, false)
	store := &fakeStorageGuardStore{size: 99}

	checkStorageGuard(store, policy)
	if store.full {
		t.Fatal("storage below the cap should remain writable")
	}
	if store.pruneCalls != 0 {
		t.Fatalf("auto-prune disabled: got %d prune calls, want 0", store.pruneCalls)
	}

	store.size = 100
	checkStorageGuard(store, policy)
	if !store.full {
		t.Fatal("storage at the cap should report full")
	}
	if store.pruneCalls != 0 {
		t.Fatalf("auto-prune disabled at cap: got %d prune calls, want 0", store.pruneCalls)
	}
}

func TestStorageGuardEnabledPrunesBeforeCap(t *testing.T) {
	policy := newStorageGuardPolicy(100, true)
	store := &fakeStorageGuardStore{size: 80, activeID: "active"}

	checkStorageGuard(store, policy)

	if store.pruneCalls != 1 {
		t.Fatalf("got %d prune calls, want 1", store.pruneCalls)
	}
	if store.pruneLimit != 70 {
		t.Fatalf("prune target = %d, want 70", store.pruneLimit)
	}
	if store.full {
		t.Fatal("successful proactive prune should leave storage writable")
	}
}

func TestStorageGuardEnabledPausesBeforeCapWhenPruneFails(t *testing.T) {
	policy := newStorageGuardPolicy(100, true)
	store := &fakeStorageGuardStore{size: 85, pruneErr: errors.New("boom")}

	checkStorageGuard(store, policy)

	if !store.full {
		t.Fatal("failed proactive prune should pause ingestion before the hard cap")
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

package main

import (
	"testing"

	"github.com/zfogg/spaniel/internal/storage"
)

func TestStorageGuardUnreachableMetadataFloorReleasesRecoveredAdmission(t *testing.T) {
	policy := newStorageGuardPolicy(500, true)
	policy.pruning.Store(true)
	policy.recovering.Store(true)
	store := &fakeStorageGuardStore{size: 355, pruneErr: storage.ErrRetentionTargetUnreached, backoff: true}
	checkStorageGuard(store, policy)
	if store.full || policy.recovering.Load() {
		t.Fatal("optional 350 target latched ingestion closed at 355 with a 500 cap")
	}
	// Backoff must not reacquire the emergency latch on the next check.
	policy.nextPruneAfter.Store(0)
	checkStorageGuard(store, policy)
	if store.full {
		t.Fatal("backoff rejected ingestion below high water")
	}
	store.size = 500
	checkStorageGuard(store, policy)
	if !store.full {
		t.Fatal("unreachable target bypassed the hard cap")
	}
}

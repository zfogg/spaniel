package main

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/zfogg/spaniel/internal/storage"
)

type fakeStorageGuardStore struct {
	size       int64
	activeID   string
	full       bool
	pruneCalls int
	pruneLimit int64
	pruneErr   error
	backoff    bool
}

func TestStorageGuardWaitsAfterSlowPassBeforeNextPause(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	const interval = 20 * time.Millisecond
	finished := make(chan time.Time, 1)
	next := make(chan time.Time, 1)
	go func() {
		first := true
		runStorageGuardChecks(ctx, interval, func() {
			if first {
				first = false
				time.Sleep(2 * interval)
				finished <- time.Now()
				return
			}
			next <- time.Now()
			cancel()
		})
	}()
	var end, start time.Time
	select {
	case end = <-finished:
	case <-time.After(2 * time.Second):
		t.Fatal("first pass stalled")
	}
	select {
	case start = <-next:
	case <-time.After(2 * time.Second):
		t.Fatal("next pass stalled")
	}
	if start.Sub(end) < interval {
		t.Fatal("overdue tick started another maintenance pause immediately")
	}
}

func TestStorageWALCheckpointPreservesModesAndPrioritizesEviction(t *testing.T) {
	for _, tc := range []struct {
		name           string
		cap            int64
		auto           bool
		size           int64
		wantCheckpoint bool
		wantFull       bool
	}{
		{"unlimited", 0, false, 200, true, false},
		{"preserve", 100, false, 90, true, false},
		{"preserve at cap", 100, false, 100, true, true},
		{"auto below high water", 100, true, 79, true, false},
		{"auto eviction priority", 100, true, 80, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &fakeStorageGuardStore{size: tc.size}
			calls := 0
			got := checkStorageWAL(store, newStorageGuardPolicy(tc.cap, tc.auto), storageWALCheckpointBytes, func(ctx context.Context) error {
				calls++
				if _, ok := ctx.Deadline(); !ok {
					t.Fatal("checkpoint has no deadline")
				}
				return nil
			})
			if got != tc.wantCheckpoint || (calls == 1) != tc.wantCheckpoint || store.full != tc.wantFull || store.pruneCalls != 0 {
				t.Fatalf("checkpoint=%v calls=%d full=%v prune=%d", got, calls, store.full, store.pruneCalls)
			}
		})
	}
}

func TestStorageWALTimeoutDoesNotHideRestorationFailure(t *testing.T) {
	for _, tc := range []struct {
		err  error
		full bool
	}{
		{context.DeadlineExceeded, false},
		{errors.Join(context.DeadlineExceeded, errors.New("reopen failed")), true},
	} {
		store := &fakeStorageGuardStore{size: 20}
		checkStorageWAL(store, newStorageGuardPolicy(100, false), storageWALCheckpointBytes, func(context.Context) error { return tc.err })
		if store.full != tc.full {
			t.Fatalf("err=%v full=%v", tc.err, store.full)
		}
	}
}

func TestStorageWALCheckpointContinuesDuringEvictionBackoff(t *testing.T) {
	store := &fakeStorageGuardStore{size: 95, backoff: true}
	called := checkStorageWAL(store, newStorageGuardPolicy(100, true), storageWALCheckpointBytes, func(context.Context) error {
		store.size = 75
		return nil
	})
	if !called || store.full || store.pruneCalls != 0 {
		t.Fatalf("WAL checkpoint starved during eviction backoff: called=%v full=%v prune=%d", called, store.full, store.pruneCalls)
	}
}

func (f *fakeStorageGuardStore) UsedSize() int64           { return f.size }
func (f *fakeStorageGuardStore) ActiveSessionID() string   { return f.activeID }
func (f *fakeStorageGuardStore) SetFull(full bool)         { f.full = full }
func (f *fakeStorageGuardStore) RetentionBackingOff() bool { return f.backoff }
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

func TestStorageGuardSoftTargetFailureKeepsBelowCapWritable(t *testing.T) {
	policy := newStorageGuardPolicy(500, true)
	for _, size := range []int64{400, 499, 500, 501} {
		store := &fakeStorageGuardStore{size: size, pruneErr: storage.ErrRetentionTargetUnreached}
		checkStorageGuard(store, policy)
		if store.full != (size >= 500) {
			t.Fatalf("size=%d full=%v", size, store.full)
		}
		if store.pruneCalls != 1 {
			t.Fatalf("size=%d repeated maintenance: %d", size, store.pruneCalls)
		}
	}
	store := &fakeStorageGuardStore{size: 450, pruneErr: errors.Join(storage.ErrRetentionTargetUnreached, errors.New("disk failure"))}
	checkStorageGuard(store, policy)
	if !store.full {
		t.Fatal("mixed genuine storage failure must remain blocked")
	}
}

package main

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/zfogg/spaniel/internal/storage"
)

type fakeStorageGuardStore struct {
	size           int64
	activeID       string
	full           bool
	pruneCalls     int
	pruneLimit     int64
	pruneErr       error
	backoff        bool
	pruneBudget    time.Duration
	requiredBudget time.Duration
	pruneSizes     []int64
	retainSize     bool
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
		{"auto below high water", 100, true, 94, true, false},
		{"auto eviction priority", 100, true, 95, false, false},
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
	f.pruneBudget = cfg.MaxDuration
	if cfg.MaxDuration < f.requiredBudget {
		return storage.PruneResult{}, storage.ErrRetentionTargetUnreached
	}
	if f.pruneErr != nil {
		return storage.PruneResult{}, f.pruneErr
	}
	if len(f.pruneSizes) > 0 {
		f.size, f.pruneSizes = f.pruneSizes[0], f.pruneSizes[1:]
	} else if !f.retainSize {
		f.size = cfg.MaxDBSizeBytes - 1
	}
	return storage.PruneResult{DeletedBySize: 2, FinalDBSizeBytes: f.size}, nil
}

func TestStorageGuardHardCapRecoveryCanFinishLargerRebuild(t *testing.T) {
	for _, size := range []int64{100, 101} {
		store := &fakeStorageGuardStore{size: size, requiredBudget: 2 * storageMaintenanceBudget}
		checkStorageGuard(store, newStorageGuardPolicy(100, true))
		if store.pruneCalls != 1 || store.pruneLimit != 70 || store.pruneBudget != storageRecoveryBudget || store.full || store.size >= 100 {
			t.Fatalf("hard-cap rebuild did not restore ingestion: %+v", store)
		}
	}
}

func TestStorageGuardProactiveMaintenanceKeepsShortBudget(t *testing.T) {
	store := &fakeStorageGuardStore{size: 99, requiredBudget: 2 * storageMaintenanceBudget}
	checkStorageGuard(store, newStorageGuardPolicy(100, true))
	if store.pruneBudget != storageMaintenanceBudget || store.full || store.size != 99 {
		t.Fatalf("proactive pass must stay short without marking an under-cap store full: %+v", store)
	}
	// Continued ingestion crosses the cap after the short attempt. The next
	// automatic pass must make forward progress rather than retry that budget.
	store.size = 100
	checkStorageGuard(store, newStorageGuardPolicy(100, true))
	if store.pruneCalls != 2 || store.full || store.size >= 100 {
		t.Fatalf("crossing the cap did not promote recovery: %+v", store)
	}
}

func TestStorageGuardSpacesBelowCapMaintenancePhases(t *testing.T) {
	policy := newStorageGuardPolicy(100, true)
	store := &fakeStorageGuardStore{size: 95, retainSize: true}

	checkStorageGuard(store, policy)
	checkStorageGuard(store, policy)
	if store.pruneCalls != 1 || store.full {
		t.Fatalf("below-cap maintenance repeated without a read window: %+v", store)
	}

	// Reaching the real cap bypasses the proactive cooldown so ingestion can
	// recover immediately with the longer emergency budget.
	store.size = 100
	checkStorageGuard(store, policy)
	if store.pruneCalls != 2 || store.pruneBudget != storageRecoveryBudget {
		t.Fatalf("hard-cap recovery was delayed by proactive cooldown: %+v", store)
	}
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
	store := &fakeStorageGuardStore{size: 95, activeID: "active"}

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
	store := &fakeStorageGuardStore{size: 95, pruneErr: errors.New("boom")}

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
	for _, size := range []int64{475, 499, 500, 501} {
		policy := newStorageGuardPolicy(500, true)
		store := &fakeStorageGuardStore{size: size, pruneErr: storage.ErrRetentionTargetUnreached}
		checkStorageGuard(store, policy)
		if store.full != (size >= 500) {
			t.Fatalf("size=%d full=%v", size, store.full)
		}
		if store.pruneCalls != 1 {
			t.Fatalf("size=%d repeated maintenance: %d", size, store.pruneCalls)
		}
	}
	policy := newStorageGuardPolicy(500, true)
	store := &fakeStorageGuardStore{size: 490, pruneErr: errors.Join(storage.ErrRetentionTargetUnreached, errors.New("disk failure"))}
	checkStorageGuard(store, policy)
	if !store.full {
		t.Fatal("mixed genuine storage failure must remain blocked")
	}
}

func TestStorageGuardRecoveryStaysPausedUntilLowWater(t *testing.T) {
	policy := newStorageGuardPolicy(100, true)
	store := &fakeStorageGuardStore{size: 101, requiredBudget: 2 * storageMaintenanceBudget, pruneSizes: []int64{99, 94, 71, 70}}
	for pass, want := range []int64{99, 94, 71, 70} {
		checkStorageGuard(store, policy)
		if store.size != want || store.pruneCalls != pass+1 || store.pruneBudget != storageRecoveryBudget {
			t.Fatalf("recovery stalled after crossing below cap/high water at pass %d: %+v", pass, store)
		}
		if store.full != (want > 70) {
			t.Fatalf("reopened before low water at pass %d: full=%v size=%d", pass, store.full, store.size)
		}
	}
	checkStorageGuard(store, policy)
	if store.pruneCalls != 4 || store.full {
		t.Fatal("low water did not finish recovery")
	}
	store.size = 94
	checkStorageGuard(store, policy)
	if store.pruneCalls != 4 {
		t.Fatal("idle storage retriggered below high water")
	}
}

func TestStorageGuardProactivePhasesContinueBelowHighWater(t *testing.T) {
	policy := newStorageGuardPolicy(100, true)
	store := &fakeStorageGuardStore{size: 95, pruneSizes: []int64{90, 80, 70}}
	for pass, want := range []int64{90, 80, 70} {
		policy.nextPruneAfter.Store(0) // elapsed read/ingest window between phases
		checkStorageGuard(store, policy)
		if store.pruneCalls != pass+1 || store.size != want || store.full {
			t.Fatalf("proactive low-water cycle stopped early at pass %d: %+v", pass, store)
		}
	}
}

func TestStorageWALCannotResumeIncompleteHardCapRecovery(t *testing.T) {
	policy := newStorageGuardPolicy(100, true)
	store := &fakeStorageGuardStore{size: 100, pruneSizes: []int64{99}}
	checkStorageGuard(store, policy)
	store.backoff = true
	called := checkStorageWAL(store, policy, storageWALCheckpointBytes, func(context.Context) error { store.size = 90; return nil })
	if !called || !store.full {
		t.Fatal("WAL checkpoint reopened ingestion before recovery reached low water")
	}
	policy.Update(100, false)
	checkStorageGuard(store, policy)
	if store.full {
		t.Fatal("disabling autoprune did not restore preserve-mode cap semantics")
	}
}

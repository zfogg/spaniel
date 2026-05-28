package forwarder

import (
	"testing"
)

func TestSpoolPersistsAcrossReopen(t *testing.T) {
	dir := t.TempDir()

	sp, err := openSpool(dir, "persist_test", 100<<20)
	if err != nil {
		t.Fatal(err)
	}

	sp.write("/v1/traces", "application/json", []byte(`{"a":1}`))
	sp.write("/v1/traces", "application/json", []byte(`{"b":2}`))
	sp.write("/v1/traces", "application/json", []byte(`{"c":3}`))

	if p := sp.pendingBytes(); p == 0 {
		t.Fatal("expected pending bytes after write")
	}
	if err := sp.f.Close(); err != nil {
		t.Fatal(err)
	}

	sp2, err := openSpool(dir, "persist_test", 100<<20)
	if err != nil {
		t.Fatal(err)
	}
	defer sp2.f.Close()

	for i := range 3 {
		rec, err := sp2.readNext()
		if err != nil {
			t.Fatalf("record %d: %v", i, err)
		}
		if rec == nil {
			t.Fatalf("record %d: expected record, got nil", i)
		}
		sp2.commit(rec)
	}

	// Fourth read should be nil — spool empty.
	rec, err := sp2.readNext()
	if err != nil {
		t.Fatal(err)
	}
	if rec != nil {
		t.Error("expected nil after all records committed")
	}

	if p := sp2.pendingBytes(); p != 0 {
		t.Errorf("expected 0 pending bytes, got %d", p)
	}
}

func TestSpoolSizeCapDropsOldest(t *testing.T) {
	dir := t.TempDir()

	// path="/v1/traces"(10) ct="application/json"(16) body=60 bytes
	// record = 2+10+2+16+4+60 = 94 bytes
	// maxBytes=100 → room for exactly one record.
	body := make([]byte, 60)

	sp, err := openSpool(dir, "cap_test", 100)
	if err != nil {
		t.Fatal(err)
	}
	defer sp.f.Close()

	sp.write("/v1/traces", "application/json", body)
	if sp.dropped.Load() != 0 {
		t.Error("expected 0 dropped after first write")
	}

	// Second write exceeds cap → first record dropped.
	sp.write("/v1/traces", "application/json", body)
	if sp.dropped.Load() != 1 {
		t.Errorf("expected 1 dropped, got %d", sp.dropped.Load())
	}

	// Only the second record should be readable.
	rec, err := sp.readNext()
	if err != nil {
		t.Fatal(err)
	}
	if rec == nil {
		t.Fatal("expected a record after cap enforcement")
	}
	sp.commit(rec)

	if p := sp.pendingBytes(); p != 0 {
		t.Errorf("expected 0 pending after commit, got %d", p)
	}
}

func TestSpoolCommitGuardAfterCapDrop(t *testing.T) {
	dir := t.TempDir()

	body := make([]byte, 60)

	sp, err := openSpool(dir, "guard_test", 100)
	if err != nil {
		t.Fatal(err)
	}
	defer sp.f.Close()

	// Write first record and read it (simulate send in flight).
	sp.write("/v1/traces", "application/json", body)
	rec, err := sp.readNext()
	if err != nil || rec == nil {
		t.Fatal("expected first record")
	}

	// Before commit: write second record, which exceeds cap and drops first.
	sp.write("/v1/traces", "application/json", body)
	if sp.dropped.Load() != 1 {
		t.Fatalf("expected 1 dropped, got %d", sp.dropped.Load())
	}

	// Commit the stale record: guard should make it a no-op.
	sp.commit(rec)

	// Second record should still be readable.
	rec2, err := sp.readNext()
	if err != nil {
		t.Fatal(err)
	}
	if rec2 == nil {
		t.Fatal("second record should still be pending after stale commit")
	}
}

func TestSpoolEmptyAfterAllCommits(t *testing.T) {
	dir := t.TempDir()

	sp, err := openSpool(dir, "empty_test", 100<<20)
	if err != nil {
		t.Fatal(err)
	}
	defer sp.f.Close()

	sp.write("/a", "text/plain", []byte("hello"))

	rec, err := sp.readNext()
	if err != nil || rec == nil {
		t.Fatal("expected record")
	}
	sp.commit(rec)

	// Spool should have been compacted.
	sp.mu.Lock()
	ro, wo := sp.readOff, sp.writeOff
	sp.mu.Unlock()
	if ro != 0 || wo != 0 {
		t.Errorf("expected offsets reset to 0 after compact, got readOff=%d writeOff=%d", ro, wo)
	}
}

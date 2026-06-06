package storage

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"syscall"
	"testing"
)

func TestIsStorageFull(t *testing.T) {
	cases := []struct {
		err  error
		want bool
	}{
		{nil, false},
		{errors.New("syntax error"), false},
		{ErrStorageFull, true},
		{fmt.Errorf("flush spans: %w", ErrStorageFull), true},
		{syscall.ENOSPC, true},
		{errors.New("IO Error: No space left on device"), true},
		{errors.New("disk is full"), true},
	}
	for _, c := range cases {
		if got := IsStorageFull(c.err); got != c.want {
			t.Errorf("IsStorageFull(%v) = %v, want %v", c.err, got, c.want)
		}
	}
}

func TestFullFlag(t *testing.T) {
	d, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	if d.Full() {
		t.Error("a new DB should not be full")
	}
	d.SetFull(true)
	if !d.Full() {
		t.Error("Full() = false after SetFull(true)")
	}
	d.SetFull(false)
	if d.Full() {
		t.Error("Full() = true after SetFull(false)")
	}
}

func TestFullFlag_SharedAcrossWithContext(t *testing.T) {
	d, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	d.SetFull(true)
	// WithContext copies the struct but must share the full state.
	if !d.WithContext(context.Background()).Full() {
		t.Error("WithContext copy did not observe the shared full flag")
	}
}

func TestFileSize(t *testing.T) {
	if d, _ := Open(":memory:"); d != nil {
		if sz := d.FileSize(); sz != 0 {
			t.Errorf(":memory: FileSize = %d, want 0", sz)
		}
		d.Close()
	}
	d, err := Open(filepath.Join(t.TempDir(), "f.duckdb"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	_, _ = d.CreateSession("s", false)
	_ = d.FlushBatch()
	if sz := d.FileSize(); sz <= 0 {
		t.Errorf("file-backed FileSize = %d, want > 0", sz)
	}
}

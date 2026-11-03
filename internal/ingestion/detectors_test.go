package ingestion

import (
	"strings"
	"testing"
)

func TestFingerprintSQLSameFingerprint(t *testing.T) {
	// Same structure with different literal values must produce the same fingerprint.
	fp1 := fingerprintSQL("SELECT * FROM users WHERE id = 1")
	fp2 := fingerprintSQL("SELECT * FROM users WHERE id = 99")
	if fp1 != fp2 {
		t.Errorf("expected same fingerprint for same structure, got %q vs %q", fp1, fp2)
	}
}

func TestFingerprintSQLDifferentTable(t *testing.T) {
	// Different table name must produce different fingerprints.
	fp1 := fingerprintSQL("SELECT * FROM users WHERE id = 1")
	fp2 := fingerprintSQL("SELECT * FROM orders WHERE id = 1")
	if fp1 == fp2 {
		t.Errorf("expected different fingerprints for different tables, but both were %q", fp1)
	}
}

func TestFingerprintSQLInsertSameFingerprint(t *testing.T) {
	fp1 := fingerprintSQL("INSERT INTO t VALUES (1, 'a', 3.14)")
	fp2 := fingerprintSQL("INSERT INTO t VALUES (99, 'z', 0.0)")
	if fp1 != fp2 {
		t.Errorf("expected same fingerprint for INSERT with different literals, got %q vs %q", fp1, fp2)
	}
}

func TestFingerprintSQLMultipleConditions(t *testing.T) {
	fp1 := fingerprintSQL("SELECT * FROM t WHERE id = 1 AND name = 'alice'")
	fp2 := fingerprintSQL("SELECT * FROM t WHERE id = 2 AND name = 'bob'")
	if fp1 != fp2 {
		t.Errorf("expected same fingerprint for WHERE with different literal values, got %q vs %q", fp1, fp2)
	}
}

func TestFingerprintSQLAlreadyNormalized(t *testing.T) {
	// Should not panic; returns something sensible (non-empty).
	result := fingerprintSQL("SELECT * FROM t WHERE sku = ?")
	if result == "" {
		t.Errorf("expected non-empty result for already-normalized SQL, got empty string")
	}
}

func TestFingerprintSQLEmptyString(t *testing.T) {
	// Should not panic.
	result := fingerprintSQL("")
	// empty input → fallback returns ""
	_ = result
}

func TestFingerprintSQLVersionedAndFailClosed(t *testing.T) {
	fp := fingerprintSQL("SELECT 'private-value'")
	if !strings.HasPrefix(fp, "sequel:v1:generic:") || strings.Contains(fp, "private-value") {
		t.Fatal("fingerprint is not a safe versioned identifier")
	}
	if fingerprintSQL("SELECT 'private-value") != "" {
		t.Fatal("malformed input has a fingerprint")
	}
	if fingerprintSQL("SELECT a, bc") == fingerprintSQL("SELECT ab, c") {
		t.Fatal("token boundaries collapsed")
	}
	if fingerprintSQL("SELECT 1") != fingerprintSQL("  SELECT   99  -- comment\n") {
		t.Fatal("literals or whitespace changed shape")
	}
}

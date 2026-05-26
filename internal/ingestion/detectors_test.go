package ingestion

import (
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

func TestFingerprintSQLUnparseable(t *testing.T) {
	// Unparseable SQL falls back to whitespace-normalized input, not a panic.
	result := fingerprintSQL("not sql at all !@#$")
	expected := "not sql at all !@#$"
	if result != expected {
		t.Errorf("expected fallback to return whitespace-normalized input %q, got %q", expected, result)
	}
}

func TestFingerprintSQLMultiSpaceNormalized(t *testing.T) {
	// Multi-space / tab input should be whitespace-normalized via fallback.
	result := fingerprintSQL("not\tsql\t\tat   all")
	expected := "not sql at all"
	if result != expected {
		t.Errorf("expected %q, got %q", expected, result)
	}
}

func TestFallbackFingerprintCollapseSpaces(t *testing.T) {
	result := fallbackFingerprint("  SELECT   *   FROM   t  ")
	expected := "SELECT * FROM t"
	if result != expected {
		t.Errorf("expected %q, got %q", expected, result)
	}
}

func TestFallbackFingerprintCollapseTabs(t *testing.T) {
	result := fallbackFingerprint("SELECT\t*\tFROM\tt")
	expected := "SELECT * FROM t"
	if result != expected {
		t.Errorf("expected %q, got %q", expected, result)
	}
}

func TestFallbackFingerprintTrimLeadingTrailing(t *testing.T) {
	result := fallbackFingerprint("  hello world  ")
	expected := "hello world"
	if result != expected {
		t.Errorf("expected %q, got %q", expected, result)
	}
}

func TestFallbackFingerprintEmpty(t *testing.T) {
	result := fallbackFingerprint("")
	if result != "" {
		t.Errorf("expected empty string, got %q", result)
	}
}

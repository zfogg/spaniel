package storage

import (
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestPhysicalUnicodeScanLeavesValidDatabaseUntouched(t *testing.T) {
	db, err := sql.Open("duckdb", filepath.Join(t.TempDir(), "spaniel.duckdb"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE sample (attributes VARCHAR, name VARCHAR); INSERT INTO sample VALUES ('{"name":"✓"}', 'valid ✓')`); err != nil {
		t.Fatal(err)
	}
	count, err := repairInvalidUnicode(db)
	if err != nil || count != 0 {
		t.Fatalf("scan: count=%d err=%v", count, err)
	}
	var value string
	if err := db.QueryRow(`SELECT name FROM sample`).Scan(&value); err != nil || value != "valid ✓" {
		t.Fatalf("value=%q err=%v", value, err)
	}
}

func TestPhysicalUnicodeRepairPreservesBytes(t *testing.T) {
	bad := "prefix" + string([]byte{0xff}) + "suffix"
	attrs, changed := repairText(bad, "VARCHAR", "attributes")
	if !changed {
		t.Fatal("invalid attributes not repaired")
	}
	name, changed := repairText(bad, "VARCHAR", "name")
	if !changed {
		t.Fatal("invalid name not repaired")
	}
	var envelope map[string]string
	if err := json.Unmarshal([]byte(attrs), &envelope); err != nil {
		t.Fatal(err)
	}
	raw, err := base64.StdEncoding.DecodeString(envelope["spaniel.invalid_utf8_base64"])
	if err != nil || string(raw) != bad {
		t.Fatalf("original bytes lost: %v", err)
	}
	if !utf8.ValidString(name) || !strings.Contains(name, `\xff`) {
		t.Fatalf("bad escaped name %q", name)
	}
	if s, changed := repairText("valid ✓", "VARCHAR", "attributes"); changed || s != "valid ✓" {
		t.Fatal("valid text was changed")
	}
}

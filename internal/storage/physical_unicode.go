package storage

import (
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

// copyPhysicalDatabaseRepairUnicode works only on a private snapshot. The
// authoritative database is never updated; if any part fails it remains
// available for recovery. Invalid bytes in structured telemetry are retained
// in a JSON envelope, and invalid bytes in other strings are escaped as hex.
func copyPhysicalDatabaseRepairUnicode(source, candidate string, dataBytes ...*int64) error {
	dir, err := os.MkdirTemp(filepath.Dir(candidate), "unicode-repair-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	clone := filepath.Join(dir, filepath.Base(source))
	for _, suffix := range []string{"", ".wal"} {
		in, e := os.Open(source + suffix)
		if os.IsNotExist(e) && suffix != "" {
			continue
		}
		if e != nil {
			return e
		}
		out, e := os.OpenFile(clone+suffix, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if e == nil {
			_, e = io.Copy(out, in)
		}
		if out != nil {
			if closeErr := out.Close(); e == nil {
				e = closeErr
			}
		}
		if closeErr := in.Close(); e == nil {
			e = closeErr
		}
		if e != nil {
			return fmt.Errorf("copy unicode repair snapshot: %w", e)
		}
	}
	db, err := sql.Open("duckdb", clone+"?threads=4&memory_limit=2GB&storage_compatibility_version=latest")
	if err != nil {
		return err
	}
	db.SetMaxOpenConns(2)
	count, err := repairInvalidUnicode(db)
	err = errors.Join(err, db.Close())
	if err != nil {
		return fmt.Errorf("repair unicode snapshot: %w", err)
	}
	if count == 0 {
		return fmt.Errorf("unicode rewrite failed, but no invalid text was found")
	}
	for _, suffix := range []string{"", ".wal"} {
		if e := os.Remove(candidate + suffix); e != nil && !os.IsNotExist(e) {
			return e
		}
	}
	slog.Warn("repaired invalid UTF-8 in retention snapshot", "values", count)
	return copyPhysicalDatabaseNative(clone, candidate, dataBytes...)
}

func repairInvalidUnicode(db *sql.DB) (int, error) {
	rows, err := db.Query(`SELECT c.table_name, c.column_name, c.data_type
		FROM information_schema.columns c JOIN information_schema.tables t
		ON c.table_catalog=t.table_catalog AND c.table_schema=t.table_schema AND c.table_name=t.table_name
		WHERE c.table_catalog=current_database() AND c.table_schema='main'
		AND t.table_type='BASE TABLE' AND c.data_type IN ('VARCHAR', 'JSON')`)
	if err != nil {
		return 0, err
	}
	type column struct{ table, name, typ string }
	var columns []column
	for rows.Next() {
		var c column
		if err = rows.Scan(&c.table, &c.name, &c.typ); err != nil {
			break
		}
		columns = append(columns, c)
	}
	if err == nil {
		err = rows.Err()
	}
	_ = rows.Close()
	if err != nil {
		return 0, err
	}
	ident := func(s string) string { return `"` + strings.ReplaceAll(s, `"`, `""`) + `"` }
	total := 0
	for _, c := range columns {
		rows, err = db.Query("SELECT rowid, " + ident(c.name) + "::VARCHAR FROM " + ident(c.table) + " WHERE " + ident(c.name) + " IS NOT NULL")
		if err != nil {
			return total, err
		}
		type fix struct {
			id    int64
			value string
		}
		var fixes []fix
		for rows.Next() {
			var id int64
			var value string
			if err = rows.Scan(&id, &value); err != nil {
				break
			}
			if fixed, changed := repairText(value, c.typ, c.name); changed {
				fixes = append(fixes, fix{id, fixed})
			}
		}
		if err == nil {
			err = rows.Err()
		}
		_ = rows.Close()
		if err != nil {
			return total, err
		}
		for _, f := range fixes {
			if _, err = db.Exec("UPDATE "+ident(c.table)+" SET "+ident(c.name)+" = ? WHERE rowid = ?", f.value, f.id); err != nil {
				return total, err
			}
			total++
		}
	}
	return total, nil
}

func repairText(value, typ, name string) (string, bool) {
	if utf8.ValidString(value) {
		return value, false
	}
	if typ == "JSON" || name == "attributes" || name == "resource" || strings.HasSuffix(name, "_attributes") {
		b, _ := json.Marshal(map[string]string{"spaniel.invalid_utf8_base64": base64.StdEncoding.EncodeToString([]byte(value))})
		return string(b), true
	}
	return escapeInvalidUTF8(value), true
}

func escapeInvalidUTF8(s string) string {
	var b strings.Builder
	for len(s) > 0 {
		r, n := utf8.DecodeRuneInString(s)
		if r == utf8.RuneError && n == 1 {
			fmt.Fprintf(&b, `\x%02x`, s[0])
		} else {
			b.WriteString(s[:n])
		}
		s = s[n:]
	}
	return b.String()
}

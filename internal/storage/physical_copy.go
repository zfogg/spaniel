package storage

import (
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

func copyPhysicalDatabase(source, candidate string) error {
	copyDB, e := sql.Open("duckdb", source+"?threads=4&memory_limit=2GB&storage_compatibility_version=latest")
	if e != nil {
		return e
	}
	copyDB.SetMaxOpenConns(1)
	var customSchema int
	if e = copyDB.QueryRow("SELECT count(*) FROM duckdb_sequences()").Scan(&customSchema); e != nil {
		_ = copyDB.Close()
		return e
	}
	if customSchema > 0 {
		_ = copyDB.Close()
		return copyPhysicalDatabasePortable(source, candidate)
	}
	quote := func(v string) string { return "'" + strings.ReplaceAll(v, "'", "''") + "'" }
	ident := func(v string) string { return `"` + strings.ReplaceAll(v, `"`, `""`) + `"` }
	var catalog string
	if e = copyDB.QueryRow("SELECT current_database()").Scan(&catalog); e != nil {
		_ = copyDB.Close()
		return e
	}
	rows, e := copyDB.Query("SELECT index_name, sql FROM duckdb_indexes() WHERE database_name=current_database() AND schema_name='main' ORDER BY index_name")
	if e != nil {
		_ = copyDB.Close()
		return e
	}
	type savedIndex struct{ name, sql string }
	var indexes []savedIndex
	for rows.Next() {
		var index savedIndex
		if e = rows.Scan(&index.name, &index.sql); e != nil {
			break
		}
		indexes = append(indexes, index)
	}
	e = errors.Join(e, rows.Err(), rows.Close())
	if e != nil {
		_ = copyDB.Close()
		return e
	}
	if _, e = copyDB.Exec("ATTACH " + quote(candidate) + " AS retention_candidate"); e == nil {
		_, e = copyDB.Exec("COPY FROM DATABASE " + ident(catalog) + " TO retention_candidate (SCHEMA)")
	}
	// The bundled engine stores catalog-qualified GORM index dependencies.
	// Restore those indexes only after the candidate is opened under the same
	// catalog name as the original file, and build each index just once.
	for _, index := range indexes {
		if e == nil {
			_, e = copyDB.Exec("DROP INDEX retention_candidate.main." + ident(index.name))
		}
	}
	if e == nil {
		_, e = copyDB.Exec("COPY FROM DATABASE " + ident(catalog) + " TO retention_candidate (DATA)")
	}
	if e == nil {
		_, e = copyDB.Exec("DETACH retention_candidate")
	}
	e = errors.Join(e, copyDB.Close())
	if e != nil {
		return fmt.Errorf("copy physical rewrite: %w", e)
	}
	copyDB, e = sql.Open("duckdb", candidate+"?threads=4&memory_limit=2GB&storage_compatibility_version=latest")
	if e != nil {
		return e
	}
	copyDB.SetMaxOpenConns(1)
	for _, index := range indexes {
		if _, e = copyDB.Exec(index.sql); e != nil {
			_ = copyDB.Close()
			return fmt.Errorf("restore physical rewrite index %s: %w", index.name, e)
		}
	}
	if _, e = copyDB.Exec("CHECKPOINT"); e != nil {
		_ = copyDB.Close()
		return e
	}
	if e = copyDB.Close(); e != nil {
		return e
	}
	return nil
}

// Custom sequence schemas retain the portable path: DuckDB cannot natively
// rebind catalog-qualified sequence defaults across attached databases.
func copyPhysicalDatabasePortable(source, candidate string) error {
	copyDB, e := sql.Open("duckdb", source+"?threads=4&memory_limit=2GB&storage_compatibility_version=latest")
	if e != nil {
		return e
	}
	copyDB.SetMaxOpenConns(1)
	quote := func(v string) string { return "'" + strings.ReplaceAll(v, "'", "''") + "'" }
	exportDir := filepath.Join(filepath.Dir(candidate), "export")
	if _, e = copyDB.Exec("EXPORT DATABASE " + quote(exportDir) + " (FORMAT PARQUET)"); e != nil {
		_ = copyDB.Close()
		return fmt.Errorf("export physical rewrite: %w", e)
	}
	if e = copyDB.Close(); e != nil {
		return e
	}
	copyDB, e = sql.Open("duckdb", candidate+"?threads=4&memory_limit=2GB&storage_compatibility_version=latest")
	if e != nil {
		return e
	}
	copyDB.SetMaxOpenConns(1)
	if _, e = copyDB.Exec("IMPORT DATABASE " + quote(exportDir)); e != nil {
		_ = copyDB.Close()
		return fmt.Errorf("import physical rewrite: %w", e)
	}
	if _, e = copyDB.Exec("CHECKPOINT"); e != nil {
		_ = copyDB.Close()
		return e
	}
	if e = copyDB.Close(); e != nil {
		return e
	}
	return nil
}

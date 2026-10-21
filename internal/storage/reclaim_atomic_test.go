package storage

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gorm.io/gorm"
)

func reclaimSnapshot(t *testing.T, d *DB) (schema, rows, indexes string) {
	t.Helper()
	for query, out := range map[string]*string{
		"SELECT sql FROM duckdb_tables() WHERE table_name='spans'": &schema,
		"SELECT CAST(to_json(list(row(trace_id,span_id,parent_span_id,service_name,name,kind,start_ns,end_ns,duration_ns,status_code,status_message,attributes,resource,session_id,session_label,received_at,sampled) ORDER BY span_id)) AS VARCHAR) FROM spans": &rows,
		"SELECT CAST(to_json(list(row(index_name,sql) ORDER BY index_name)) AS VARCHAR) FROM duckdb_indexes() WHERE table_name='spans'":                                                                                                                          &indexes,
	} {
		if err := d.SQL().QueryRow(query).Scan(out); err != nil {
			t.Fatal(err)
		}
	}
	return
}

func TestRetentionAtomicRebuildPreservesSchemaRowsIndexesAndReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "spaniel.duckdb")
	d, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	seedSession(t, d, "atomic", "atomic", time.Now().UnixNano(), false, 10000)
	if err := d.gorm.Exec("DELETE FROM spans WHERE span_id LIKE '%0'").Error; err != nil {
		t.Fatal(err)
	}
	schema, rows, indexes := reclaimSnapshot(t, d)
	if err := d.withMaintenance(func(d *DB) error { return d.reclaimRetentionSpace() }); err != nil {
		t.Fatal(err)
	}
	afterSchema, afterRows, afterIndexes := reclaimSnapshot(t, d)
	if schema != afterSchema || rows != afterRows || indexes != afterIndexes {
		t.Fatal("compaction changed schema, payload or lookup indexes")
	}
	if err := d.Close(); err != nil {
		t.Fatal(err)
	}
	d, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	afterSchema, afterRows, afterIndexes = reclaimSnapshot(t, d)
	if schema != afterSchema || rows != afterRows || indexes != afterIndexes {
		t.Fatal("restart changed compacted schema/payload/indexes")
	}
	if err := d.gorm.Exec("INSERT INTO spans(trace_id,span_id) VALUES('default','default')").Error; err != nil {
		t.Fatal(err)
	}
	var sampled bool
	if err := d.SQL().QueryRow("SELECT sampled FROM spans WHERE span_id='default'").Scan(&sampled); err != nil {
		t.Fatal(err)
	}
	if !sampled {
		t.Fatal("sampled default lost")
	}
	if err := d.AppendSpan(&Span{TraceID: "rebound", SpanID: "rebound", Attributes: `{}`, Resource: `{}`}); err != nil {
		t.Fatal(err)
	}
	if err := d.FlushBatch(); err != nil {
		t.Fatal(err)
	}
}

func TestRetentionAtomicRebuildRollbackKeepsOriginalAndAppenders(t *testing.T) {
	d := openTestDB(t)
	seedSession(t, d, "atomic", "atomic", time.Now().UnixNano(), false, 1000)
	if err := d.gorm.Exec("DELETE FROM spans WHERE span_id LIKE '%0'").Error; err != nil {
		t.Fatal(err)
	}
	schema, rows, indexes := reclaimSnapshot(t, d)
	forced := errors.New("cancel replacement before commit")
	replaced := false
	d.gorm.Callback().Raw().After("gorm:raw").Register("fail_replacement", func(tx *gorm.DB) {
		if strings.HasPrefix(tx.Statement.SQL.String(), `ALTER TABLE "spaniel_retention_copy_spans" RENAME TO "spans"`) {
			replaced = true
			tx.AddError(forced)
		}
	})
	err := d.withMaintenance(func(d *DB) error { return d.reclaimRetentionSpace() })
	if !replaced || !errors.Is(err, forced) {
		t.Fatalf("did not exercise replacement rollback: %v", err)
	}
	afterSchema, afterRows, afterIndexes := reclaimSnapshot(t, d)
	if schema != afterSchema || rows != afterRows || indexes != afterIndexes {
		t.Fatal("rollback changed original")
	}
	if err := d.AppendSpan(&Span{TraceID: "rebound", SpanID: "rebound", Attributes: `{}`, Resource: `{}`}); err != nil {
		t.Fatal(err)
	}
	if err := d.FlushBatch(); err != nil {
		t.Fatal(err)
	}
}

func TestRetentionAtomicRebuildKeepsLogAndHistogramPayloads(t *testing.T) {
	d := openTestDB(t)
	for i := range 4 {
		if err := d.AppendLog(&Log{TimestampNs: int64(i), TraceID: "trace", Body: "kept Unicode Ω", Attributes: `{"nested":{"value":1}}`, SessionID: "session"}); err != nil {
			t.Fatal(err)
		}
		if err := d.AppendMetric(&Metric{Name: "duration", ServiceName: "api", SessionID: "session", TimestampNs: int64(i), Type: "histogram", Attributes: `{"raw":true}`, Resource: `{"host.name":"host"}`, SeriesAttributes: `{"route":"/cart"}`, SeriesKey: "cart", ScopeAttributes: `{"nested":{"value":1}}`, ExplicitBounds: "[1,10]", BucketCounts: "[1,2,0]", Exemplars: `[{"trace_id":"trace","span_id":"span"}]`}); err != nil {
			t.Fatal(err)
		}
	}
	if err := d.FlushBatch(); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"logs", "metrics"} {
		if err := d.gorm.Exec("DELETE FROM " + table + " WHERE timestamp_ns=0").Error; err != nil {
			t.Fatal(err)
		}
	}
	snapshot := func() []string {
		result := []string{}
		for _, table := range []string{"logs", "metrics"} {
			var value string
			if err := d.SQL().QueryRow("SELECT CAST(to_json(list(t ORDER BY timestamp_ns)) AS VARCHAR) FROM " + table + " t").Scan(&value); err != nil {
				t.Fatal(err)
			}
			result = append(result, value)
		}
		return result
	}
	before := snapshot()
	if err := d.withMaintenance(func(d *DB) error { return d.reclaimRetentionSpace() }); err != nil {
		t.Fatal(err)
	}
	after := snapshot()
	for i := range before {
		if before[i] != after[i] {
			t.Fatal("log/metric observations or metadata changed")
		}
	}
}

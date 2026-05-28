package storage

import (
	"testing"
	"time"
)

func TestPragmaStorageInfo2(t *testing.T) {
	db := openTestDB(t)
	sess, _ := db.CreateSession("si2-test", false)
	for i := range 5 {
		db.InsertSpan(&Span{
			TraceID: "t" + string(rune('0'+i)), SpanID: "s" + string(rune('0'+i)),
			SessionID: sess.ID, Name: "op", ServiceName: "svc",
			Attributes: `{"key":"a very long attribute value to contribute bytes"}`,
			Resource: `{"service.name":"svc"}`,
			StartNs: 1000, EndNs: 2000, ReceivedAt: time.Now().UnixNano(),
		})
	}
	type row struct {
		ColumnName  string
		SegmentID   int64
		SegmentType string
		SegmentInfo string
		Persistent  bool
		Stats       string
	}
	var rows []row
	db.gorm.Raw(`SELECT column_name, segment_id, segment_type, segment_info, persistent, stats FROM pragma_storage_info('spans')`).Scan(&rows)
	for _, r := range rows {
		t.Logf("col=%q type=%s info=%s persistent=%v stats=%s", r.ColumnName, r.SegmentType, r.SegmentInfo, r.Persistent, r.Stats)
	}
	
	// Also try duckdb_memory_usage or similar
	var mem []struct{ Name string; Value string }
	db.gorm.Raw(`SELECT * FROM duckdb_memory_usage()`).Scan(&mem)
	t.Logf("memory_usage: %+v", mem)
}

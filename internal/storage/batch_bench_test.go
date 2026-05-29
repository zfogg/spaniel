package storage

import (
	"fmt"
	"testing"
)

func benchSpan(i int) *Span {
	return &Span{
		TraceID: fmt.Sprintf("trace-%d", i/10), SpanID: fmt.Sprintf("span-%d", i),
		ParentSpanID: "", ServiceName: "checkout", Name: "GET /cart",
		Kind: 2, StartNs: int64(i) * 1000, EndNs: int64(i)*1000 + 500,
		StatusCode: 0, Attributes: `{"http.method":"GET","http.route":"/cart","http.status_code":200}`,
		Resource: `{"service.name":"checkout","service.version":"1.4.2"}`,
		SessionID: "bench", Sampled: true,
	}
}

// BenchmarkInsertSpanGORM measures the old per-row gorm.Create write path.
func BenchmarkInsertSpanGORM(b *testing.B) {
	db, err := Open(":memory:")
	if err != nil {
		b.Fatal(err)
	}
	defer db.Close()
	b.ResetTimer()
	for i := 0; b.Loop(); i++ {
		if err := db.InsertSpan(benchSpan(i)); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkAppendSpanBatched measures the new Appender write path, flushing
// once per 512-span batch to mirror a typical OTLP export request.
func BenchmarkAppendSpanBatched(b *testing.B) {
	db, err := Open(":memory:")
	if err != nil {
		b.Fatal(err)
	}
	defer db.Close()
	b.ResetTimer()
	for i := 0; b.Loop(); i++ {
		if err := db.AppendSpan(benchSpan(i)); err != nil {
			b.Fatal(err)
		}
		if i%512 == 511 {
			if err := db.FlushBatch(); err != nil {
				b.Fatal(err)
			}
		}
	}
	if err := db.FlushBatch(); err != nil {
		b.Fatal(err)
	}
}

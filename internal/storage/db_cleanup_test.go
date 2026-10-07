package storage

import "testing"

func TestDeleteSpansNamedRemovesOnlyMatchingSpanDependencies(t *testing.T) {
	d := openTestDB(t)
	for _, span := range []*Span{
		{SpanID: "remove", TraceID: "trace", Name: "db.query", SessionID: "session"},
		{SpanID: "keep", TraceID: "trace", Name: "storage.ListTraces", SessionID: "session"},
	} {
		if err := d.InsertSpan(span); err != nil {
			t.Fatalf("insert span: %v", err)
		}
	}
	if err := d.InsertSpanEvents([]*SpanEvent{{SpanID: "remove", SessionID: "session", Attributes: "{}"}, {SpanID: "keep", SessionID: "session", Attributes: "{}"}}); err != nil {
		t.Fatalf("insert events: %v", err)
	}
	if err := d.InsertSpanLinks([]*SpanLink{{SpanID: "remove", SessionID: "session", Attributes: "{}"}, {SpanID: "keep", LinkedSpanID: "remove", SessionID: "session", Attributes: "{}"}, {SpanID: "keep", SessionID: "session", Attributes: "{}"}}); err != nil {
		t.Fatalf("insert links: %v", err)
	}

	deleted, err := d.DeleteSpansNamed("db.query")
	if err != nil {
		t.Fatalf("delete db.query spans: %v", err)
	}
	if deleted != 1 {
		t.Fatalf("deleted = %d, want 1", deleted)
	}
	if got, err := d.GetSpan("remove"); err != nil || got != nil {
		t.Errorf("removed span = %#v, %v; want nil, nil", got, err)
	}
	if got, err := d.GetSpan("keep"); err != nil || got == nil {
		t.Errorf("kept span = %#v, %v; want present", got, err)
	}
	var events, links int64
	if err := d.gorm.Model(&SpanEvent{}).Count(&events).Error; err != nil {
		t.Fatalf("count events: %v", err)
	}
	if err := d.gorm.Model(&SpanLink{}).Count(&links).Error; err != nil {
		t.Fatalf("count links: %v", err)
	}
	if events != 1 || links != 1 {
		t.Errorf("dependencies remaining: events=%d links=%d, want 1 each", events, links)
	}
}

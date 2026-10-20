package ingestion

import (
	"fmt"
	"testing"
	"time"

	"github.com/zfogg/spaniel/internal/storage"
)

func serverGetSpan(id string) *storage.Span {
	return &storage.Span{
		SpanID:     id,
		Kind:       2, // SERVER
		Attributes: `{"http.method":"GET","http.route":"/api/items"}`,
		StartNs:    0, EndNs: int64(300 * time.Millisecond),
		SessionID: "sess",
	}
}

func inlineDBSpan(id, parentID string) *storage.Span {
	return &storage.Span{
		SpanID:       id,
		ParentSpanID: parentID,
		Attributes:   `{"db.system":"postgresql","db.statement":"SELECT 1"}`,
		StartNs:      0, EndNs: int64(20 * time.Millisecond),
		SessionID: "sess",
	}
}

func TestSynchronousIO_ThreeSerialDBChildrenFire(t *testing.T) {
	root := serverGetSpan("root")
	spans := []*storage.Span{root}
	for i := range 3 {
		child := inlineDBSpan(fmt.Sprintf("db%d", i), "root")
		child.StartNs = int64(i*20) * int64(time.Millisecond)
		child.EndNs = int64((i+1)*20) * int64(time.Millisecond)
		spans = append(spans, child)
	}
	d := &SynchronousIODetector{}
	got := d.Analyze("t", "s", spans, 0)
	if len(got) != 1 {
		t.Fatalf("expected 1 issue for GET + 3 DB children, got %d", len(got))
	}
	if got[0].Kind != "synchronous_io" {
		t.Errorf("wrong kind: %q", got[0].Kind)
	}
	if got[0].Count != 3 {
		t.Errorf("Count = %d, want 3", got[0].Count)
	}
}

func TestSynchronousIO_UsesNonoverlappingDBPhases(t *testing.T) {
	tests := []struct {
		name   string
		calls  [][2]int
		phases int
		waitMs int64
	}{
		{"three parallel calls", [][2]int{{0, 20}, {0, 20}, {0, 20}}, 0, 0},
		{"pair lookup then parallel reads", [][2]int{{0, 5}, {10, 30}, {10, 25}, {10, 35}}, 0, 0},
		{"transitive overlap", [][2]int{{0, 30}, {20, 50}, {40, 60}, {50, 70}}, 0, 0},
		{"long call overlaps short sequential calls", [][2]int{{0, 100}, {1, 2}, {3, 4}, {5, 6}}, 0, 0},
		{"three waves of parallel calls", [][2]int{{0, 20}, {0, 15}, {30, 50}, {30, 45}, {60, 80}, {60, 75}}, 3, 60},
		{"adjacent sequential calls", [][2]int{{0, 20}, {20, 40}, {40, 60}}, 3, 60},
		{"unordered input", [][2]int{{60, 80}, {0, 20}, {30, 50}}, 3, 60},
		{"calls outside request", [][2]int{{400, 420}, {430, 450}, {460, 480}}, 0, 0},
		{"invalid durations do not add phases", [][2]int{{0, 20}, {30, 50}, {60, 60}, {70, 65}}, 0, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spans := []*storage.Span{serverGetSpan("root")}
			for i, bounds := range tt.calls {
				child := inlineDBSpan(fmt.Sprintf("db%d", i), "root")
				child.StartNs, child.EndNs = int64(bounds[0])*int64(time.Millisecond), int64(bounds[1])*int64(time.Millisecond)
				spans = append(spans, child)
			}
			got := (&SynchronousIODetector{}).Analyze("trace", "sess", spans, 0)
			if tt.phases == 0 {
				if len(got) != 0 {
					t.Fatalf("concurrent/nonblocking work reported as serial: %+v", got)
				}
				return
			}
			if len(got) != 1 || got[0].Count != tt.phases || got[0].WastedNs != tt.waitMs*int64(time.Millisecond) {
				t.Fatalf("expected %d phases / %dms, got %+v", tt.phases, tt.waitMs, got)
			}
		})
	}
}

func TestSynchronousIO_ExcludesOtherServiceClockAndNonDBChildren(t *testing.T) {
	root := serverGetSpan("root")
	root.ServiceName = "api"
	spans := []*storage.Span{root}
	for i := range 3 {
		child := inlineDBSpan(fmt.Sprintf("remote%d", i), "root")
		child.ServiceName = "remote-database"
		child.StartNs = int64(i*30) * int64(time.Millisecond)
		child.EndNs = child.StartNs + int64(20*time.Millisecond)
		spans = append(spans, child)
	}
	for i := range 3 {
		child := inlineDBSpan(fmt.Sprintf("internal%d", i), "root")
		child.ServiceName = "api"
		child.Attributes = "{}"
		spans = append(spans, child)
	}
	if got := (&SynchronousIODetector{}).Analyze("trace", "sess", spans, 0); len(got) != 0 {
		t.Fatalf("nonlocal or non-DB children counted as handler SQL: %+v", got)
	}
}

func TestSynchronousIO_TwoDBChildren_NoFire(t *testing.T) {
	root := serverGetSpan("root")
	spans := []*storage.Span{root,
		inlineDBSpan("db0", "root"),
		inlineDBSpan("db1", "root"),
	}
	d := &SynchronousIODetector{}
	if got := d.Analyze("t", "s", spans, 0); len(got) != 0 {
		t.Errorf("2 DB children should not fire, got %d", len(got))
	}
}

func TestSynchronousIO_PostMethod_NoFire(t *testing.T) {
	root := &storage.Span{
		SpanID: "root", Kind: 2,
		Attributes: `{"http.method":"POST"}`,
		StartNs:    0, EndNs: int64(200 * time.Millisecond), SessionID: "sess",
	}
	spans := []*storage.Span{root}
	for i := range 3 {
		spans = append(spans, inlineDBSpan(fmt.Sprintf("db%d", i), "root"))
	}
	d := &SynchronousIODetector{}
	if got := d.Analyze("t", "s", spans, 0); len(got) != 0 {
		t.Errorf("POST method should not fire, got %d", len(got))
	}
}

func TestSynchronousIO_ClientSpan_NoFire(t *testing.T) {
	// Kind 3 = CLIENT — not a server span.
	root := &storage.Span{
		SpanID: "root", Kind: 3,
		Attributes: `{"http.method":"GET"}`,
		StartNs:    0, EndNs: int64(200 * time.Millisecond), SessionID: "sess",
	}
	spans := []*storage.Span{root}
	for i := range 3 {
		spans = append(spans, inlineDBSpan(fmt.Sprintf("db%d", i), "root"))
	}
	d := &SynchronousIODetector{}
	if got := d.Analyze("t", "s", spans, 0); len(got) != 0 {
		t.Errorf("client span should not fire, got %d", len(got))
	}
}

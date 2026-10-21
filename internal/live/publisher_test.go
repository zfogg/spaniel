package live

import (
	"testing"
	"time"

	"github.com/zfogg/spaniel/internal/ws"
)

func TestPublisherDoesNotQueryWithoutViewers(t *testing.T) {
	// No DB is necessary: a publisher without consumers must never access it.
	p := NewPublisher(nil, ws.NewHub(), nil, nil)
	p.Notify()
	if p.timer != nil {
		t.Fatal("scheduled a database summary without viewers")
	}
	// A previously scheduled timer must also stop when its last viewer leaves.
	p.timer = time.NewTimer(time.Hour)
	p.timer.Stop()
	p.publish()
	if p.timer != nil {
		t.Fatal("retained a summary timer without viewers")
	}
}

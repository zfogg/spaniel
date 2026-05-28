package ws

import (
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

// Typed payload structs

type SpanPayload struct {
	TraceID     string `json:"traceId"`
	SpanID      string `json:"spanId"`
	ServiceName string `json:"serviceName"`
	Name        string `json:"name"`
	DurationNs  int64  `json:"durationNs"`
	StatusCode  int    `json:"statusCode"`
}

type LogPayload struct {
	TraceID     string `json:"traceId"`
	SpanID      string `json:"spanId"`
	Severity    int    `json:"severity"`
	Body        string `json:"body"`
	ServiceName string `json:"serviceName"`
	SessionID   string `json:"sessionId"`
}

type MetricPayload struct {
	Name        string  `json:"name"`
	ServiceName string  `json:"serviceName"`
	Value       float64 `json:"value"`
	Type        string  `json:"type"`
}

type IssuePayload struct {
	TraceID     string `json:"traceId"`
	Kind        string `json:"kind"`
	Fingerprint string `json:"fingerprint"`
	Count       int    `json:"count"`
	WastedNs    int64  `json:"wastedNs"`
}

type ForwarderPayload struct {
	URL          string `json:"url"`
	Sent         int64  `json:"sent"`
	Errors       int64  `json:"errors"`
	LastErr      string `json:"lastError,omitempty"`
	PendingBytes int64  `json:"pendingBytes,omitempty"`
	DroppedSpool int64  `json:"droppedSpool,omitempty"`
}

type ThroughputPayload struct {
	SpansPerSec float64 `json:"spansPerSec"`
	LogsPerSec  float64 `json:"logsPerSec"`
}

// Event is the outer discriminated union sent over the WebSocket.
type Event struct {
	Type      string `json:"type"`
	Timestamp int64  `json:"timestamp_ns"`
	Payload   any    `json:"payload"`
}

// SpanEvent is kept as a backward-compat alias for SpanPayload.
type SpanEvent = SpanPayload

// Constructor helpers

func NewSpanEvent(p *SpanPayload) *Event {
	return &Event{Type: "span", Timestamp: time.Now().UnixNano(), Payload: p}
}

func NewLogEvent(p *LogPayload) *Event {
	return &Event{Type: "log", Timestamp: time.Now().UnixNano(), Payload: p}
}

func NewMetricEvent(p *MetricPayload) *Event {
	return &Event{Type: "metric", Timestamp: time.Now().UnixNano(), Payload: p}
}

func NewIssueEvent(p *IssuePayload) *Event {
	return &Event{Type: "issue", Timestamp: time.Now().UnixNano(), Payload: p}
}

func NewForwarderEvent(p *ForwarderPayload) *Event {
	return &Event{Type: "forwarder", Timestamp: time.Now().UnixNano(), Payload: p}
}

func NewThroughputEvent(p *ThroughputPayload) *Event {
	return &Event{Type: "throughput", Timestamp: time.Now().UnixNano(), Payload: p}
}

type Hub struct {
	mu      sync.RWMutex
	clients map[*websocket.Conn]struct{}
}

func NewHub() *Hub {
	return &Hub{
		clients: make(map[*websocket.Conn]struct{}),
	}
}

func (h *Hub) ServeWS(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	h.mu.Lock()
	h.clients[conn] = struct{}{}
	h.mu.Unlock()

	go func() {
		defer func() {
			h.mu.Lock()
			delete(h.clients, conn)
			h.mu.Unlock()
			conn.Close()
		}()
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				break
			}
		}
	}()
}

func (h *Hub) Broadcast(ev *Event) {
	data, err := json.Marshal(ev)
	if err != nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for conn := range h.clients {
		conn.WriteMessage(websocket.TextMessage, data) //nolint:errcheck
	}
}

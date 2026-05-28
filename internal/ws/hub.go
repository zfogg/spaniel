package ws

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
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

	bytesSent     metric.Int64Counter
	bytesReceived metric.Int64Counter
}

func NewHub() *Hub {
	meter := otel.Meter("spaniel/ws")
	sent, _ := meter.Int64Counter("spaniel.ws.bytes_sent",
		metric.WithDescription("Total bytes sent to WebSocket clients"),
		metric.WithUnit("By"),
	)
	received, _ := meter.Int64Counter("spaniel.ws.bytes_received",
		metric.WithDescription("Total bytes received from WebSocket clients"),
		metric.WithUnit("By"),
	)
	h := &Hub{
		clients:       make(map[*websocket.Conn]struct{}),
		bytesSent:     sent,
		bytesReceived: received,
	}
	_, _ = meter.Int64ObservableGauge("spaniel.ws.connections",
		metric.WithDescription("Active WebSocket connections"),
		metric.WithUnit("{connection}"),
		metric.WithInt64Callback(func(_ context.Context, o metric.Int64Observer) error {
			h.mu.RLock()
			o.Observe(int64(len(h.clients)))
			h.mu.RUnlock()
			return nil
		}),
	)
	return h
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
			_, msg, err := conn.ReadMessage()
			if err != nil {
				break
			}
			h.bytesReceived.Add(context.Background(), int64(len(msg)))
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
	h.bytesSent.Add(context.Background(), int64(len(data))*int64(len(h.clients)))
}

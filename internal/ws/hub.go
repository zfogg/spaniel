package ws

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/coder/websocket"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
)

const (
	// clientSendBuffer is how many queued messages a single client may fall
	// behind by before it is considered too slow and dropped. Sized to absorb
	// short bursts without unbounded memory growth.
	clientSendBuffer = 256
	// writeTimeout bounds a single frame write so one stalled client cannot
	// wedge its writer goroutine forever.
	writeTimeout = 5 * time.Second
	// defaultHeartbeatInterval is how often each client gets a heartbeat frame.
	// It keeps the connection warm and lets the client detect a dead link by
	// noticing the absence of frames (the JS WebSocket API can't observe
	// protocol-level pings, so this is an application message).
	defaultHeartbeatInterval = 5 * time.Second
)

// Typed payload structs

type SpanPayload struct {
	TraceID     string `json:"traceId"`
	SpanID      string `json:"spanId"`
	ServiceName string `json:"serviceName"`
	Name        string `json:"name"`
	DurationNs  int64  `json:"durationNs"`
	StatusCode  int    `json:"statusCode"`
	SessionID   string `json:"sessionId"`
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

// NewHeartbeatEvent is a payload-less liveness beat the server sends on a timer.
// Clients use its arrival (and the silence when it stops) to track connectivity.
func NewHeartbeatEvent() *Event {
	return &Event{Type: "heartbeat", Timestamp: time.Now().UnixNano()}
}

// client is one connected WebSocket peer. Each has a buffered send channel
// drained by a dedicated writer goroutine, so a slow consumer never blocks
// Broadcast (and therefore never blocks the ingestion goroutines that call it).
// cancel tears the connection down — invoked when the buffer overflows, a write
// fails, or the peer disconnects.
type client struct {
	conn   *websocket.Conn
	send   chan []byte
	cancel context.CancelFunc
}

type Hub struct {
	mu      sync.RWMutex
	clients map[*client]struct{}

	// heartbeatInterval is how often each client's writePump emits a heartbeat.
	// Set by NewHub; overridable in tests.
	heartbeatInterval time.Duration

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
		clients:           make(map[*client]struct{}),
		heartbeatInterval: defaultHeartbeatInterval,
		bytesSent:         sent,
		bytesReceived:     received,
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
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		// spaniel is a local dev tool; accept any origin (mirrors the previous
		// gorilla CheckOrigin → true).
		InsecureSkipVerify: true,
	})
	if err != nil {
		return
	}

	// Connection lifetime is governed by this context, independent of the HTTP
	// request (which coder/websocket detaches after Accept).
	ctx, cancel := context.WithCancel(context.Background())
	c := &client{
		conn:   conn,
		send:   make(chan []byte, clientSendBuffer),
		cancel: cancel,
	}

	h.mu.Lock()
	h.clients[c] = struct{}{}
	h.mu.Unlock()

	// The writer goroutine owns all writes (coder/websocket forbids concurrent
	// writes). readPump blocks here for the connection's lifetime, keeping the
	// HTTP handler alive until the peer disconnects or the client is dropped.
	go h.writePump(ctx, c)
	h.readPump(ctx, c)

	cancel()
	h.mu.Lock()
	delete(h.clients, c)
	h.mu.Unlock()
	_ = conn.CloseNow()
}

// readPump drains inbound frames (clients don't send anything meaningful; this
// is for byte accounting and to observe the close handshake) until the
// connection errors or ctx is canceled.
func (h *Hub) readPump(ctx context.Context, c *client) {
	for {
		_, data, err := c.conn.Read(ctx)
		if err != nil {
			return
		}
		h.bytesReceived.Add(ctx, int64(len(data)))
	}
}

// writePump is the single writer for a client, draining its send buffer. A
// write failure (or canceled ctx) cancels the client so readPump unblocks and
// ServeWS tears the connection down.
func (h *Hub) writePump(ctx context.Context, c *client) {
	ticker := time.NewTicker(h.heartbeatInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			data, err := json.Marshal(NewHeartbeatEvent())
			if err != nil {
				continue
			}
			if !h.writeFrame(ctx, c, data) {
				return
			}
		case data := <-c.send:
			if !h.writeFrame(ctx, c, data) {
				return
			}
		}
	}
}

// writeFrame writes one text frame with the write timeout, accounting for bytes
// sent. It returns false (after cancelling the client) when the write fails.
func (h *Hub) writeFrame(ctx context.Context, c *client, data []byte) bool {
	writeCtx, cancel := context.WithTimeout(ctx, writeTimeout)
	err := c.conn.Write(writeCtx, websocket.MessageText, data)
	cancel()
	if err != nil {
		c.cancel()
		return false
	}
	h.bytesSent.Add(ctx, int64(len(data)))
	return true
}

func (h *Hub) Broadcast(ev *Event) {
	data, err := json.Marshal(ev)
	if err != nil {
		return
	}
	// RLock: broadcasts run concurrently and only enqueue into per-client
	// buffered channels — they never write to the socket directly, so a slow
	// client can't stall this loop. A client whose buffer is full is dropped
	// (cancel triggers async teardown) rather than blocking everyone.
	h.mu.RLock()
	for c := range h.clients {
		select {
		case c.send <- data:
		default:
			c.cancel()
		}
	}
	h.mu.RUnlock()
}

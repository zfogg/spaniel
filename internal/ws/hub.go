package ws

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/zfogg/spaniel/internal/liveevent"
	"github.com/zfogg/spaniel/internal/telemetry"
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

// Compatibility aliases; new consumers import internal/liveevent.
type SpanPayload = liveevent.SpanPayload
type SelfTracePayload = liveevent.SelfTracePayload
type SelfSpanPayload = liveevent.SelfSpanPayload
type SelfLogPayload = liveevent.SelfLogPayload
type LogPayload = liveevent.LogPayload
type MetricPayload = liveevent.MetricPayload
type IssuePayload = liveevent.IssuePayload
type ForwarderPayload = liveevent.ForwarderPayload
type ThroughputPayload = liveevent.ThroughputPayload
type LiveStatePayload = liveevent.LiveStatePayload
type ActiveSessionPayload = liveevent.ActiveSessionPayload
type Event = liveevent.Event
type SpanEvent = liveevent.SpanEvent

var NewSpanEvent = liveevent.NewSpanEvent
var NewSelfTraceEvent = liveevent.NewSelfTraceEvent
var NewSelfSpanEvent = liveevent.NewSelfSpanEvent
var NewSelfLogEvent = liveevent.NewSelfLogEvent
var NewLogEvent = liveevent.NewLogEvent
var NewMetricEvent = liveevent.NewMetricEvent
var NewIssueEvent = liveevent.NewIssueEvent
var NewForwarderEvent = liveevent.NewForwarderEvent
var NewThroughputEvent = liveevent.NewThroughputEvent
var NewLiveStateEvent = liveevent.NewLiveStateEvent
var NewActiveSessionEvent = liveevent.NewActiveSessionEvent
var NewHeartbeatEvent = liveevent.NewHeartbeatEvent

// client is one connected WebSocket peer. Each has a buffered send channel
// drained by a dedicated writer goroutine, so a slow consumer never blocks
// Broadcast (and therefore never blocks the ingestion goroutines that call it).
// cancel tears the connection down — invoked when the buffer overflows, a write
// fails, or the peer disconnects.
type client struct {
	conn   *websocket.Conn
	send   chan []byte
	cancel context.CancelFunc
	// Guarded by Hub.mu. Older clients remain visible until they explicitly
	// send a visibility frame, preserving compatibility with existing viewers.
	hidden bool
	ready  bool // opt-in registration confirmation for snapshot clients
}

type Hub struct {
	mu      sync.RWMutex
	clients map[*client]struct{}

	// heartbeatInterval is how often each client's writePump emits a heartbeat.
	// Set by NewHub; overridable in tests.
	heartbeatInterval time.Duration
}

func NewHub() *Hub {
	h := &Hub{
		clients:           make(map[*client]struct{}),
		heartbeatInterval: defaultHeartbeatInterval,
	}
	return h
}

// HasViewers reports whether UI-derived database work currently has a visible
// consumer. Background sockets stay connected for alerts and notifications.
func (h *Hub) HasViewers() bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for c := range h.clients {
		if !c.hidden {
			return true
		}
	}
	return false
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
		ready:  r.URL.Query().Get("ready") == "1",
	}

	h.mu.Lock()
	h.clients[c] = struct{}{}
	clientCount := int64(len(h.clients))
	h.mu.Unlock()
	telemetry.Catalog().SetWebSocketClients(clientCount)
	telemetry.Catalog().RecordWebSocketClientConnected(context.Background())

	// The writer goroutine owns all writes (coder/websocket forbids concurrent
	// writes). readPump blocks here for the connection's lifetime, keeping the
	// HTTP handler alive until the peer disconnects or the client is dropped.
	go h.writePump(ctx, c)
	h.readPump(ctx, c)

	cancel()
	h.mu.Lock()
	delete(h.clients, c)
	clientCount = int64(len(h.clients))
	h.mu.Unlock()
	telemetry.Catalog().SetWebSocketClients(clientCount)
	_ = conn.CloseNow()
}

// readPump accounts for incoming frames and records the viewer's visibility.
// Unknown client messages are ignored; alert delivery remains connected while
// a background tab opts out of UI updates and derived database snapshots.
func (h *Hub) readPump(ctx context.Context, c *client) {
	for {
		_, data, err := c.conn.Read(ctx)
		if err != nil {
			return
		}
		telemetry.Catalog().RecordWebSocketMessageReceived(ctx, int64(len(data)))
		var state struct {
			Type    string `json:"type"`
			Visible *bool  `json:"visible"`
		}
		if json.Unmarshal(data, &state) == nil && state.Type == "visibility" && state.Visible != nil {
			h.mu.Lock()
			c.hidden = !*state.Visible
			h.mu.Unlock()
		}
	}
}

// writePump is the single writer for a client, draining its send buffer. A
// write failure (or canceled ctx) cancels the client so readPump unblocks and
// ServeWS tears the connection down.
func (h *Hub) writePump(ctx context.Context, c *client) {
	// Confirm registration before clients bootstrap their HTTP snapshots.
	if c.ready {
		data, _ := json.Marshal(NewHeartbeatEvent())
		if !h.writeFrame(ctx, c, data) {
			return
		}
	}
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
		telemetry.Catalog().RecordWebSocketClientDropped(context.Background(), "write_failed")
		c.cancel()
		return false
	}
	telemetry.Catalog().RecordWebSocketMessageSent(ctx, "frame", int64(len(data)))
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
	queued, dropped := int64(0), int64(0)
	for c := range h.clients {
		if c.hidden && ev.Type != "alert" && ev.Type != "alert_sync" && ev.Type != "notification" && ev.Type != "definition_sync" && ev.Type != "resources_changed" {
			continue
		}
		select {
		case c.send <- data:
			queued++
		default:
			c.cancel()
			dropped++
		}
	}
	h.mu.RUnlock()
	if queued > 0 {
		telemetry.Catalog().RecordWebSocket(context.Background(), ev.Type, "queued", queued)
	}
	if dropped > 0 {
		telemetry.Catalog().RecordWebSocket(context.Background(), ev.Type, "dropped", dropped)
		for range dropped {
			telemetry.Catalog().RecordWebSocketClientDropped(context.Background(), "slow_consumer")
		}
	}
}

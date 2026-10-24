package ingestion

import (
	"context"
	"errors"
	"sync"
	"time"

	"go.opentelemetry.io/collector/pdata/plog"
	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.opentelemetry.io/collector/pdata/ptrace"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/zfogg/spaniel/internal/telemetry"
	"github.com/zfogg/spaniel/internal/ws"
)

// These limits cover queued AND executing exports. Bytes are protobuf size,
// not a claim about the Go heap size of decoded pdata or HTTP request bodies.
const (
	batchWindow   = time.Second
	batchRows     = 4096
	batchBytes    = 4 << 20
	queueRequests = 256
	queueBytes    = 64 << 20
)

var (
	ErrIngestTooLarge = errors.New("export exceeds configured ingestion request limit")
	ErrIngestBusy     = errors.New("ingestion queue capacity exceeded; retry export")
	ErrIngestClosed   = errors.New("ingestion is shutting down; retry export")
)

type batchWork struct {
	ctx         context.Context
	run         func(context.Context) error
	rows, bytes int
	queued      time.Time
	done        chan struct{}
	err         error
	started     bool
	finished    bool
}

// The worker is lazy and exits when idle, so embedders need not maintain an
// idle goroutine. All ownership transitions, including cancellation, use mu.
type ingestBatcher struct {
	limitRequests, limitBytes int
	mu                        sync.Mutex
	pending                   []*batchWork
	requests, bytes           int
	running, closed           bool
	wake                      chan struct{}
	idle                      chan struct{}
	window                    time.Duration
	flush                     func(context.Context, bool) error
	failureVersion            func() uint64
	notify                    func()
}

func newIngestBatcher(flush func(context.Context, bool) error, notify func()) *ingestBatcher {
	return &ingestBatcher{wake: make(chan struct{}, 1), window: batchWindow, limitRequests: queueRequests, limitBytes: queueBytes, flush: flush, notify: notify}
}

func (b *ingestBatcher) signal() {
	select {
	case b.wake <- struct{}{}:
	default:
	}
}

func (b *ingestBatcher) enqueue(ctx context.Context, rows, size int, run func(context.Context) error) (*batchWork, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if b.closed {
		return nil, ErrIngestClosed
	}
	if b.requests >= b.limitRequests || size > b.limitBytes-b.bytes {
		return nil, ErrIngestBusy
	}
	w := &batchWork{ctx: ctx, run: run, rows: rows, bytes: size, queued: time.Now(), done: make(chan struct{})}
	b.pending = append(b.pending, w)
	b.requests++
	b.bytes += size
	telemetry.Catalog().SetIngestBatchQueue(b.requests, b.limitRequests, b.bytes)
	if !b.running {
		b.running = true
		b.idle = make(chan struct{})
		go b.loop()
	}
	b.signal()
	return w, nil
}

func (b *ingestBatcher) release(w *batchWork) {
	w.finished = true
	b.requests--
	b.bytes -= w.bytes
	telemetry.Catalog().SetIngestBatchQueue(b.requests, b.limitRequests, b.bytes)
	w.run = nil
	close(w.done)
}

func (b *ingestBatcher) wait(w *batchWork) error {
	select {
	case <-w.done:
		return w.err
	case <-w.ctx.Done():
		b.mu.Lock()
		if !w.started && !w.finished {
			for i, pending := range b.pending {
				if pending == w {
					b.pending = append(b.pending[:i], b.pending[i+1:]...)
					break
				}
			}
			w.err = w.ctx.Err()
			b.release(w)
			b.signal()
		}
		b.mu.Unlock()
		// Once processing starts we own the caller's pdata until run completes.
		// Do not let cancellation return it to an exporter while still in use.
		<-w.done
		return w.err
	}
}

func (b *ingestBatcher) loop() {
	for {
		b.mu.Lock()
		if len(b.pending) == 0 {
			b.running = false
			close(b.idle)
			b.mu.Unlock()
			return
		}
		rows, size, n := 0, 0, 0
		for _, w := range b.pending {
			if n > 0 && (rows+w.rows > batchRows || size+w.bytes > batchBytes) {
				break
			}
			rows += w.rows
			size += w.bytes
			n++
			if rows >= batchRows || size >= batchBytes {
				break
			}
		}
		remaining := time.Until(b.pending[0].queued.Add(b.window))
		ready := b.closed || remaining <= 0 || rows >= batchRows || size >= batchBytes || n < len(b.pending) || b.requests >= b.limitRequests
		if !ready {
			b.mu.Unlock()
			timer := time.NewTimer(remaining)
			select {
			case <-timer.C:
			case <-b.wake:
			}
			timer.Stop()
			continue
		}
		group := append([]*batchWork(nil), b.pending[:n]...)
		b.pending = append([]*batchWork(nil), b.pending[n:]...)
		b.mu.Unlock()
		b.execute(group)
	}
}

type sessionContextKey struct{}
type ingestSession struct{ id, label string }
type batchContextKey struct{}
type batchFailureContextKey struct{}
type batchEffects struct {
	after      []func()
	complete   []func(error)
	live       bool
	flushCtx   context.Context
	instrument bool
}

func effects(ctx context.Context) *batchEffects {
	v, _ := ctx.Value(batchContextKey{}).(*batchEffects)
	return v
}

func (b *ingestBatcher) execute(group []*batchWork) {
	size := 0
	for _, w := range group {
		size += w.bytes
	}
	telemetry.Catalog().RecordIngestGroup(len(group), size)
	stages := make([]*batchEffects, len(group))
	flushCtx := context.Background()
	var version uint64
	if b.failureVersion != nil {
		version = b.failureVersion()
		flushCtx = context.WithValue(flushCtx, batchFailureContextKey{}, version)
	}
	instrument := false
	var writeErr error
	for i, w := range group {
		b.mu.Lock()
		if w.finished {
			b.mu.Unlock()
			continue
		}
		w.started = true
		b.mu.Unlock()
		stage := &batchEffects{}
		stages[i] = stage
		telemetry.Catalog().RecordIngestQueueWait(w.ctx, float64(time.Since(w.queued).Microseconds())/1000)
		if err := w.ctx.Err(); err != nil {
			w.err = err
			continue
		}
		ctx := w.ctx
		if b.failureVersion != nil {
			ctx = context.WithValue(ctx, batchFailureContextKey{}, version)
		}
		w.err = w.run(context.WithValue(ctx, batchContextKey{}, stage))
		if writeErr == nil {
			writeErr = w.err
		}
		if stage.instrument {
			instrument = true
			flushCtx = context.WithValue(context.WithoutCancel(stage.flushCtx), batchContextKey{}, (*batchEffects)(nil))
		}
	}
	// Flush even after an append error: earlier requests and partial rows may
	// already have been appended. No export is acknowledged before this barrier.
	flushErr := errors.Join(writeErr, b.flush(flushCtx, instrument))
	live := false
	for i, w := range group {
		if stages[i] == nil {
			continue
		}
		w.err = errors.Join(w.err, flushErr)
		for _, complete := range stages[i].complete {
			complete(w.err)
		}
		if w.err == nil {
			for _, after := range stages[i].after {
				after()
			}
			live = live || stages[i].live
		}
	}
	if live {
		b.notify()
	}
	b.mu.Lock()
	for _, w := range group {
		if !w.finished {
			b.release(w)
		}
	}
	b.mu.Unlock()
}

// Close rejects new exports and drains accepted work without waiting for the
// collection window. Call before closing storage. A timeout does not discard
// accepted work; callers must still keep storage open until draining finishes.
func (b *ingestBatcher) Close(ctx context.Context) error {
	b.mu.Lock()
	b.closed = true
	idle, running := b.idle, b.running
	b.signal()
	b.mu.Unlock()
	if !running {
		return nil
	}
	select {
	case <-idle:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (p *Pipeline) Close(ctx context.Context) error { return p.batches.Close(ctx) }

func (p *Pipeline) submit(ctx context.Context, signal string, rows, size int, run func(context.Context) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if size > p.MaxRequestBytes() {
		p.rejected.Add(1)
		return ErrIngestTooLarge
	}
	ctx = context.WithValue(ctx, sessionContextKey{}, ingestSession{p.store.ActiveSessionID(), p.store.ActiveSessionLabel()})
	w, err := p.batches.enqueue(ctx, rows, size, run)
	if err != nil {
		p.rejected.Add(1)
		telemetry.Catalog().RecordIngestRejected(ctx, signal, "batch_admission")
		return err
	}
	return p.batches.wait(w)
}

func (p *Pipeline) IngestTraces(ctx context.Context, td ptrace.Traces) error {
	p.filterTraces(td)
	return p.submit(ctx, "traces", td.SpanCount(), (&ptrace.ProtoMarshaler{}).TracesSize(td), func(ctx context.Context) error { return p.ingestTraces(ctx, td) })
}
func (p *Pipeline) IngestLogs(ctx context.Context, ld plog.Logs) error {
	p.filterLogs(ld)
	return p.submit(ctx, "logs", ld.LogRecordCount(), (&plog.ProtoMarshaler{}).LogsSize(ld), func(ctx context.Context) error { return p.ingestLogs(ctx, ld) })
}
func (p *Pipeline) IngestMetrics(ctx context.Context, md pmetric.Metrics) error {
	p.filterMetrics(md)
	return p.submit(ctx, "metrics", md.DataPointCount(), (&pmetric.ProtoMarshaler{}).MetricsSize(md), func(ctx context.Context) error { return p.ingestMetrics(ctx, md) })
}

func (p *Pipeline) afterPersist(ctx context.Context, after func()) {
	if stage := effects(ctx); stage != nil {
		stage.after = append(stage.after, after)
	} else {
		after()
	}
}

func (p *Pipeline) endIngest(ctx context.Context, span trace.Span) {
	if stage := effects(ctx); stage != nil {
		stage.complete = append(stage.complete, func(err error) {
			if err != nil {
				span.RecordError(err)
				span.SetStatus(codes.Error, err.Error())
			}
			span.End()
		})
	} else {
		span.End()
	}
}
func (p *Pipeline) broadcast(ctx context.Context, event *ws.Event) {
	p.afterPersist(ctx, func() { p.hub.Broadcast(event) })
}
func (p *Pipeline) notifyPersisted(ctx context.Context) {
	if stage := effects(ctx); stage != nil {
		stage.live = true
	} else {
		p.notifyLiveState()
	}
}

func (p *Pipeline) ingestSession(ctx context.Context) ingestSession {
	if session, ok := ctx.Value(sessionContextKey{}).(ingestSession); ok {
		return session
	}
	return ingestSession{p.store.ActiveSessionID(), p.store.ActiveSessionLabel()}
}

func (p *Pipeline) flushStore(ctx context.Context) error {
	if version, ok := ctx.Value(batchFailureContextKey{}).(uint64); ok {
		return p.store.FlushBatchSince(version)
	}
	return p.store.FlushBatch()
}

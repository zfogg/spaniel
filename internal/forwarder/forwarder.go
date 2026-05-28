package forwarder

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"math/rand/v2"
	"net/http"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"

	"github.com/zfogg/spaniel/internal/goroutine"
)

// SpoolConfig controls the disk-backed retry buffer.  Zero values cause
// NewWithSpool to use its built-in defaults.
type SpoolConfig struct {
	Dir      string        // spool directory; default: ~/.spaniel/forward
	MaxBytes int64         // max pending bytes per upstream; default: 100 MB
	RetryMax time.Duration // max backoff between retries; default: 30s
}

type upstream struct {
	url        string
	sent       atomic.Int64
	errors     atomic.Int64
	skipped    atomic.Int64
	lastErr    atomic.Value // stores string
	// spool is non-nil when disk-backed retry is enabled for this upstream.
	sp         *spool
	lastFailAt atomic.Int64 // unix ns of last send failure (0 = never)
	backoffNs  atomic.Int64 // current backoff duration in ns
}

// Status is the serialisable snapshot of one upstream's counters.
type Status struct {
	URL           string `json:"url"`
	Sent          int64  `json:"sent"`
	Errors        int64  `json:"errors"`
	Skipped       int64  `json:"skipped"`
	LastErr       string `json:"last_error,omitempty"`
	PendingBytes  int64  `json:"pending_bytes,omitempty"`
	DroppedSpool  int64  `json:"dropped_spool,omitempty"`
	LastFailureAt int64  `json:"last_failure_at,omitempty"`
	BackoffNs     int64  `json:"backoff_ns,omitempty"`
}

// Forwarder fans out OTLP payloads to one or more upstream HTTP endpoints.
type Forwarder struct {
	upstreams []*upstream
	client    *http.Client
	sample    float64
	rand      func() float64
}

// New returns a Forwarder in fire-and-forget mode (no spool, no retry).
// urls should be bare base URLs, e.g. "http://tempo:4318".
// sample is the per-payload forwarding probability in [0, 1]; values outside
// the range are clamped (NaN → 1.0).
func New(urls []string, sample float64) *Forwarder {
	sample = clampSample(sample)
	ups := make([]*upstream, len(urls))
	for i, u := range urls {
		ups[i] = &upstream{url: u}
	}
	return &Forwarder{
		upstreams: ups,
		client:    &http.Client{Timeout: 5 * time.Second},
		sample:    sample,
		rand:      rand.Float64,
	}
}

// NewWithSpool returns a Forwarder backed by a per-upstream disk spool.
// Failed deliveries are retried with exponential backoff (250ms → sc.RetryMax).
// Each upstream gets a send-loop goroutine that runs for the process lifetime.
func NewWithSpool(urls []string, sample float64, sc SpoolConfig) *Forwarder {
	sample = clampSample(sample)
	if sc.MaxBytes <= 0 {
		sc.MaxBytes = 100 << 20
	}
	if sc.RetryMax <= 0 {
		sc.RetryMax = 30 * time.Second
	}
	if sc.Dir == "" {
		home, _ := os.UserHomeDir()
		sc.Dir = filepath.Join(home, ".spaniel", "forward")
	}

	ups := make([]*upstream, len(urls))
	for i, u := range urls {
		up := &upstream{url: u}
		if sp, err := openSpool(sc.Dir, urlHash(u), sc.MaxBytes); err == nil {
			up.sp = sp
		}
		ups[i] = up
	}

	f := &Forwarder{
		upstreams: ups,
		client:    &http.Client{Timeout: 5 * time.Second},
		sample:    sample,
		rand:      rand.Float64,
	}
	for _, up := range ups {
		if up.sp != nil {
			up := up // capture
			goroutine.Go(func() { f.runLoop(up, sc.RetryMax) }, "subsystem", "forwarder", "upstream", up.url)
		}
	}
	return f
}

// Forward asynchronously delivers body to each upstream at <base>/path.
// When a spool is configured the payload is written to disk first and the
// send loop handles delivery; otherwise a goroutine fires immediately.
// Per-upstream sampling is applied before any I/O.
func (f *Forwarder) Forward(path, contentType string, body []byte) {
	for _, up := range f.upstreams {
		if f.sample < 1 && f.rand() >= f.sample {
			up.skipped.Add(1)
			continue
		}
		if up.sp != nil {
			up.sp.write(path, contentType, body)
		} else {
			up, path, contentType, body := up, path, contentType, body // capture
			goroutine.Go(func() { f.send(up, path, contentType, body) }, "subsystem", "forwarder")
		}
	}
}

// send is the fire-and-forget path (no spool).
func (f *Forwarder) send(up *upstream, path, contentType string, body []byte) {
	target := up.url + path
	req, err := http.NewRequest(http.MethodPost, target, bytes.NewReader(body))
	if err != nil {
		up.errors.Add(1)
		up.lastErr.Store(err.Error())
		return
	}
	req.Header.Set("Content-Type", contentType)

	resp, err := f.client.Do(req)
	if err != nil {
		up.errors.Add(1)
		up.lastErr.Store(err.Error())
		return
	}
	resp.Body.Close()

	if resp.StatusCode >= 400 {
		msg := fmt.Sprintf("upstream returned %d", resp.StatusCode)
		up.errors.Add(1)
		up.lastErr.Store(msg)
		return
	}
	up.sent.Add(1)
}

// sendRecord sends a single spool record and returns any delivery error.
func (f *Forwarder) sendRecord(up *upstream, rec *spoolRecord) error {
	target := up.url + rec.path
	req, err := http.NewRequest(http.MethodPost, target, bytes.NewReader(rec.body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", rec.ct)

	resp, err := f.client.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("upstream returned %d", resp.StatusCode)
	}
	return nil
}

// runLoop is the per-upstream send loop.  It drains the spool oldest-first,
// retrying failures with exponential backoff and ±25 % jitter.
func (f *Forwarder) runLoop(up *upstream, retryMax time.Duration) {
	backoff := 250 * time.Millisecond
	for {
		rec, err := up.sp.readNext()
		if err != nil {
			time.Sleep(time.Second)
			continue
		}
		if rec == nil {
			select {
			case <-up.sp.notify:
			case <-time.After(5 * time.Second):
			}
			continue
		}

		if err := f.sendRecord(up, rec); err != nil {
			up.errors.Add(1)
			up.lastErr.Store(err.Error())
			up.lastFailAt.Store(time.Now().UnixNano())
			up.backoffNs.Store(int64(backoff))
			// sleep backoff/2 .. backoff with uniform jitter
			half := int64(backoff / 2)
			sleep := backoff/2 + time.Duration(rand.Int64N(half+1))
			time.Sleep(sleep)
			backoff *= 2
			if backoff > retryMax {
				backoff = retryMax
			}
		} else {
			up.sp.commit(rec)
			up.sent.Add(1)
			backoff = 250 * time.Millisecond
			up.lastFailAt.Store(0)
			up.backoffNs.Store(0)
		}
	}
}

// Status returns a snapshot of each upstream's counters.
func (f *Forwarder) Status() []Status {
	out := make([]Status, len(f.upstreams))
	for i, up := range f.upstreams {
		s := Status{
			URL:     up.url,
			Sent:    up.sent.Load(),
			Errors:  up.errors.Load(),
			Skipped: up.skipped.Load(),
		}
		if v, ok := up.lastErr.Load().(string); ok {
			s.LastErr = v
		}
		if up.sp != nil {
			s.PendingBytes  = up.sp.pendingBytes()
			s.DroppedSpool  = up.sp.dropped.Load()
			s.LastFailureAt = up.lastFailAt.Load()
			s.BackoffNs     = up.backoffNs.Load()
		}
		out[i] = s
	}
	return out
}

func clampSample(sample float64) float64 {
	if !(sample >= 0) { // catches NaN
		return 1.0
	}
	if sample > 1 {
		return 1
	}
	return sample
}

func urlHash(url string) string {
	h := sha256.Sum256([]byte(url))
	return fmt.Sprintf("%x", h[:8])
}

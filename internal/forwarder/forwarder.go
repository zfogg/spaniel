package forwarder

import (
	"bytes"
	"fmt"
	"net/http"
	"sync/atomic"
	"time"
)

type upstream struct {
	url     string
	sent    atomic.Int64
	errors  atomic.Int64
	lastErr atomic.Value // stores string
}

// Status is the serialisable snapshot of one upstream's counters.
type Status struct {
	URL     string `json:"url"`
	Sent    int64  `json:"sent"`
	Errors  int64  `json:"errors"`
	LastErr string `json:"last_error,omitempty"`
}

// Forwarder fans out OTLP payloads to one or more upstream HTTP endpoints.
type Forwarder struct {
	upstreams []*upstream
	client    *http.Client
}

// New returns a Forwarder that will POST to each of the given base URLs.
// urls should be bare base URLs, e.g. "http://tempo:4318".
func New(urls []string) *Forwarder {
	ups := make([]*upstream, len(urls))
	for i, u := range urls {
		ups[i] = &upstream{url: u}
	}
	return &Forwarder{
		upstreams: ups,
		client:    &http.Client{Timeout: 5 * time.Second},
	}
}

// Forward asynchronously POSTs body to each upstream at <base>/path.
// It is non-blocking: each upstream gets its own goroutine.
func (f *Forwarder) Forward(path, contentType string, body []byte) {
	for _, up := range f.upstreams {
		up := up
		go f.send(up, path, contentType, body)
	}
}

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

// Status returns a snapshot of each upstream's counters.
func (f *Forwarder) Status() []Status {
	out := make([]Status, len(f.upstreams))
	for i, up := range f.upstreams {
		s := Status{
			URL:    up.url,
			Sent:   up.sent.Load(),
			Errors: up.errors.Load(),
		}
		if v, ok := up.lastErr.Load().(string); ok {
			s.LastErr = v
		}
		out[i] = s
	}
	return out
}

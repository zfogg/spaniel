package client

import (
	"context"
	"github.com/zfogg/spaniel/internal/liveevent"
	"time"
)

type Key struct{ Owner, Name string }
type Fetch func(context.Context, *Client, string) (any, error)
type Spec struct {
	Key       Key
	Resources []liveevent.Resource
	Fetch     Fetch
	// Interval is opt-in for time-window results. Zero means event driven only.
	Interval time.Duration
}
type Result struct {
	Key             Key
	Sequence, Epoch uint64
	Session         string
	Data            any
	Err             error
	Pending         bool // a durable change raced this snapshot; a repair is queued
}
type Work struct {
	Result
	run Fetch
	ctx context.Context
	api *Client
}

func (w Work) Run() Result { r := w.Result; r.Data, r.Err = w.run(w.ctx, w.api, r.Session); return r }

type entry struct {
	Spec
	sequence       uint64
	dirty, running bool
	due            time.Time
	cancel         context.CancelFunc
	replay         []liveevent.Message
}

// Refreshes is owned by the UI event loop. It bounds concurrent requests and
// coalesces bursts to one pending refresh per subscription. Feature code must
// register/replace on the UI goroutine, never from a tea.Cmd.
type Refreshes struct {
	ctx      context.Context
	api      *Client
	entries  map[Key]*entry
	session  string
	epoch    uint64
	running  int
	sequence uint64
}

func NewRefreshes(ctx context.Context, api *Client) *Refreshes {
	return &Refreshes{ctx: ctx, api: api, entries: map[Key]*entry{}}
}
func (r *Refreshes) Register(s Spec) {
	if s.Fetch == nil {
		panic("watch refresh requires Fetch")
	}
	r.Remove(s.Key)
	r.entries[s.Key] = &entry{Spec: s, dirty: true}
}
func (r *Refreshes) Remove(k Key) {
	if e := r.entries[k]; e != nil {
		if e.cancel != nil {
			e.cancel()
		}
		delete(r.entries, k)
	}
}
func (r *Refreshes) Invalidate(resources []liveevent.Resource) {
	for _, e := range r.entries {
		for _, a := range resources {
			for _, b := range e.Resources {
				if a == b {
					e.dirty = true
				}
			}
		}
	}
}

// Observe runs before UI dispatch. Retain at most 256 rows per in-flight
// snapshot so applying that snapshot cannot erase newer streamed rows. The
// dirty bit still schedules an authoritative repair if the buffer overflows.
func (r *Refreshes) Observe(m liveevent.Message) {
	r.Invalidate(liveevent.Changes(m))
	affected := liveevent.Affects(m)
	for _, e := range r.entries {
		if !e.running {
			continue
		}
		matches := false
		for _, a := range affected {
			for _, b := range e.Resources {
				if a == b {
					matches = true
				}
			}
		}
		if matches {
			if len(e.replay) < 256 {
				e.replay = append(e.replay, m)
			} else {
				if len(liveevent.Changes(m)) > 0 {
					e.dirty = true
				}
				e.replay = append(e.replay[1:], m)
			}
		}
	}
}

func (r *Refreshes) Pending(k Key) bool { e := r.entries[k]; return e != nil && e.dirty }
func (r *Refreshes) DrainReplay(k Key) []liveevent.Message {
	e := r.entries[k]
	if e == nil {
		return nil
	}
	out := e.replay
	e.replay = nil
	return out
}
func (r *Refreshes) Session() string { return r.session }
func (r *Refreshes) SetSession(id string) {
	if id != r.session {
		r.session = id
		r.Resync()
	}
}
func (r *Refreshes) Resync() {
	r.epoch++
	for _, e := range r.entries {
		e.dirty = true
		e.due = time.Time{}
		if e.cancel != nil {
			e.cancel()
		}
	}
}
func (r *Refreshes) Due(now time.Time) []Work {
	var work []Work
	for _, e := range r.entries {
		if r.running >= 4 {
			break
		}
		if e.running || now.Before(e.due) || (!e.dirty && e.Interval == 0) {
			continue
		}
		r.sequence++
		e.sequence = r.sequence
		e.running = true
		e.dirty = false
		e.replay = nil
		r.running++
		ctx, cancel := context.WithCancel(r.ctx)
		e.cancel = cancel
		work = append(work, Work{Result: Result{Key: e.Key, Sequence: e.sequence, Epoch: r.epoch, Session: r.session}, run: e.Fetch, ctx: ctx, api: r.api})
	}
	return work
}

// Complete releases the global slot even for a removed/replaced subscription.
// A request from an old connection/session/selection cannot overwrite new data.
func (r *Refreshes) Complete(result Result, now time.Time) bool {
	r.running = max(0, r.running-1)
	e := r.entries[result.Key]
	if e == nil || result.Sequence != e.sequence {
		return false
	}
	e.running = false
	if e.cancel != nil {
		e.cancel()
		e.cancel = nil
	}
	if result.Epoch != r.epoch || result.Session != r.session {
		e.dirty = true
		return false
	}
	delay := time.Second
	if e.Interval > 0 {
		delay = max(delay, e.Interval)
	}
	if result.Err != nil {
		e.dirty = true
		delay = max(delay, 5*time.Second)
	}
	e.due = now.Add(delay)
	return true
}

func (r *Refreshes) RemoveOwner(owner string) {
	for k := range r.entries {
		if k.Owner == owner {
			r.Remove(k)
		}
	}
}

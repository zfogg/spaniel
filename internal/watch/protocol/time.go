package protocol

import (
	"sync/atomic"
	"time"
)

// Clock is a shared render snapshot, advanced by the shell's existing loop.
// It adds neither a timer per row nor another transport/subscription.
type Clock struct{ value atomic.Value }

func NewClock(now time.Time) *Clock { clock := &Clock{}; clock.Set(now); return clock }
func (c *Clock) Set(now time.Time)  { c.value.Store(now) }
func (c *Clock) Now() time.Time {
	if now := c.value.Load(); now != nil {
		return now.(time.Time)
	}
	return time.Now()
}
func (env Env) Now() time.Time {
	if env.Time != nil {
		return env.Time.Now()
	}
	return time.Now()
}

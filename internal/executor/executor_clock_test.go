package executor_test

import (
	"time"
)

// epoch is the start of every fake clock.
var epoch = time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

// frozenNow is a clock that never moves: every step takes 0ms.
func frozenNow() time.Time { return epoch }

// fakeClock is a clock the test moves by hand; the executor reads it, a timedBackend advances it.
type fakeClock struct{ t time.Time }

func newFakeClock() *fakeClock { return &fakeClock{t: epoch} }

// Now is the executor's clock.
func (c *fakeClock) Now() time.Time { return c.t }

// advance moves the clock forward.
func (c *fakeClock) advance(d time.Duration) { c.t = c.t.Add(d) }

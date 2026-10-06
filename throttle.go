// throttle.go — Rate-limiting frequent UI updates.
//
// latestValueThrottle keeps fast producers, such as FFmpeg progress lines
// from several concurrent post-processing jobs, from flooding the UI thread
// with one fyne.Do call per update.
package main

import (
	"sync"
	"time"
)

// statusThrottleInterval is the minimum time between two status label
// updates, i.e. at most about 7 updates per second.
const statusThrottleInterval = 150 * time.Millisecond

// latestValueThrottle applies only the newest of a stream of values, at most
// once per interval, and skips a value equal to the one applied last. A value
// set after a quiet period is applied at once; values set during the
// interval after an apply are coalesced into one apply at its end.
type latestValueThrottle[T comparable] struct {
	interval time.Duration
	// apply receives each value to show. It runs on a timer goroutine (or on
	// the goroutine calling Flush), so it must do its own fyne.Do.
	apply func(T)

	// afterFunc and now are time.AfterFunc and time.Now, replaced in tests.
	afterFunc func(time.Duration, func()) *time.Timer
	now       func() time.Time

	mu         sync.Mutex
	pending    T           // newest value set
	hasPending bool        // false until the first Set
	applied    T           // value applied last
	hasApplied bool        // false until the first apply
	timer      *time.Timer // pending apply, or nil
	armed      bool        // true while an apply is scheduled
	lastApply  time.Time   // when the last apply ran
}

// newLatestValueThrottle returns a throttle that passes values to apply at
// most once per interval.
func newLatestValueThrottle[T comparable](interval time.Duration, apply func(T)) *latestValueThrottle[T] {
	return &latestValueThrottle[T]{
		interval:  interval,
		apply:     apply,
		afterFunc: time.AfterFunc,
		now:       time.Now,
	}
}

// Set records value as the newest one and schedules an apply if none is
// scheduled yet. Safe to call from any goroutine, including the UI thread.
func (throttle *latestValueThrottle[T]) Set(value T) {
	throttle.mu.Lock()
	defer throttle.mu.Unlock()
	throttle.pending, throttle.hasPending = value, true
	if throttle.armed {
		return
	}
	throttle.armed = true
	wait := max(throttle.interval-throttle.now().Sub(throttle.lastApply), 0)
	throttle.timer = throttle.afterFunc(wait, throttle.Flush)
}

// Flush applies the newest value now, unless it equals the value applied
// last. The scheduled apply calls it; call it directly to make sure the
// newest value is shown before moving on, e.g. at the end of a session.
func (throttle *latestValueThrottle[T]) Flush() {
	throttle.mu.Lock()
	defer throttle.mu.Unlock()
	if throttle.timer != nil {
		throttle.timer.Stop()
		throttle.timer = nil
	}
	throttle.armed = false
	throttle.lastApply = throttle.now()
	if !throttle.hasPending || (throttle.hasApplied && throttle.pending == throttle.applied) {
		return
	}
	throttle.applied, throttle.hasApplied = throttle.pending, true
	// Applying under the lock keeps applies in order when a scheduled apply
	// and a direct Flush run at the same time.
	throttle.apply(throttle.pending)
}

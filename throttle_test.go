package main

import (
	"slices"
	"testing"
	"time"
)

// fakeThrottleClock drives a latestValueThrottle by hand: it records the
// applies the throttle schedules instead of running them, and supplies the
// current time.
type fakeThrottleClock struct {
	now     time.Time
	waits   []time.Duration
	pending []func()
}

func (clock *fakeThrottleClock) install(throttle *latestValueThrottle[string]) {
	throttle.now = func() time.Time { return clock.now }
	throttle.afterFunc = func(wait time.Duration, apply func()) *time.Timer {
		clock.waits = append(clock.waits, wait)
		clock.pending = append(clock.pending, apply)
		return nil
	}
}

// runScheduled runs the scheduled applies, as if their timers had fired.
func (clock *fakeThrottleClock) runScheduled() {
	scheduled := clock.pending
	clock.pending = nil
	for _, apply := range scheduled {
		apply()
	}
}

func newTestThrottle() (*latestValueThrottle[string], *fakeThrottleClock, *[]string) {
	var applied []string
	throttle := newLatestValueThrottle(statusThrottleInterval, func(value string) {
		applied = append(applied, value)
	})
	clock := &fakeThrottleClock{now: time.Unix(1000, 0)}
	clock.install(throttle)
	return throttle, clock, &applied
}

func TestThrottleCoalescesToNewestValue(t *testing.T) {
	throttle, clock, applied := newTestThrottle()

	throttle.Set("a")
	throttle.Set("b")
	throttle.Set("c")

	if len(clock.pending) != 1 {
		t.Fatalf("applies scheduled = %d, want 1 for a burst", len(clock.pending))
	}
	if clock.waits[0] != 0 {
		t.Errorf("first apply waits %v, want 0 after a quiet period", clock.waits[0])
	}
	clock.runScheduled()
	if !slices.Equal(*applied, []string{"c"}) {
		t.Errorf("applied = %q, want only the newest value", *applied)
	}
}

func TestThrottleWaitsOutTheInterval(t *testing.T) {
	throttle, clock, applied := newTestThrottle()
	throttle.Set("a")
	clock.runScheduled()

	clock.now = clock.now.Add(50 * time.Millisecond)
	throttle.Set("b")

	if want := statusThrottleInterval - 50*time.Millisecond; clock.waits[1] != want {
		t.Errorf("second apply waits %v, want %v (the rest of the interval)", clock.waits[1], want)
	}
	clock.runScheduled()
	if !slices.Equal(*applied, []string{"a", "b"}) {
		t.Errorf("applied = %q, want a then b", *applied)
	}
}

func TestThrottleSkipsRepeatedValue(t *testing.T) {
	throttle, clock, applied := newTestThrottle()
	throttle.Set("same")
	clock.runScheduled()

	clock.now = clock.now.Add(time.Second)
	throttle.Set("same")
	clock.runScheduled()

	if !slices.Equal(*applied, []string{"same"}) {
		t.Errorf("applied = %q, want the repeat skipped", *applied)
	}
}

func TestThrottleFlushAppliesNow(t *testing.T) {
	throttle, clock, applied := newTestThrottle()

	throttle.Flush()
	if len(*applied) != 0 {
		t.Fatalf("applied = %q before any Set, want nothing", *applied)
	}

	throttle.Set("final")
	throttle.Flush()
	if !slices.Equal(*applied, []string{"final"}) {
		t.Errorf("applied = %q, want the value applied by Flush", *applied)
	}

	// The apply scheduled by Set finds nothing new.
	clock.runScheduled()
	if len(*applied) != 1 {
		t.Errorf("applied = %q, want no second apply", *applied)
	}
}

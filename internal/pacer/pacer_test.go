package pacer

import (
	"testing"
	"time"
)

func TestNewRejectsBadConfiguration(t *testing.T) {
	origin := time.Now()

	cases := []struct {
		name   string
		kind   Kind
		period time.Duration
		opts   Options
	}{
		{"zero period", AbsoluteDeadline, 0, DefaultOptions()},
		{"negative period", AbsoluteDeadline, -time.Millisecond, DefaultOptions()},
		{"unknown kind", Kind("wishful-thinking"), 20 * time.Millisecond, DefaultOptions()},
		{"spin without slack", SpinTail, 20 * time.Millisecond, Options{SpinSlack: 0}},
		{"slack exceeds period", SpinTail, 20 * time.Millisecond, Options{SpinSlack: 20 * time.Millisecond}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p, err := New(c.kind, c.period, origin, c.opts)
			if err == nil {
				p.Stop()
				t.Fatalf("New(%q, %v) succeeded, want error", c.kind, c.period)
			}
		})
	}
}

func TestEveryKindConstructs(t *testing.T) {
	origin := time.Now()
	for _, k := range Kinds() {
		p, err := New(k, 20*time.Millisecond, origin, DefaultOptions())
		if err != nil {
			t.Fatalf("New(%q) failed: %v", k, err)
		}
		if p.Name() != string(k) {
			t.Errorf("Name() = %q, want %q", p.Name(), k)
		}
		p.Stop()
	}
}

// busyFor burns CPU for at least d. Simulated encode work must spin rather
// than sleep: sleeping would measure the platform's timer granularity a second
// time inside the work, which is the very thing the pacer is being judged on.
func busyFor(d time.Duration) {
	start := time.Now()
	for time.Since(start) < d {
	}
}

// lateness returns, for each tick, how far past its nominal deadline the tick
// actually woke. Tick n is nominally due at origin + n*period.
func lateness(t *testing.T, k Kind, period, work time.Duration, ticks int) []time.Duration {
	t.Helper()

	origin := time.Now()
	p, err := New(k, period, origin, DefaultOptions())
	if err != nil {
		t.Fatalf("New(%q): %v", k, err)
	}
	defer p.Stop()

	out := make([]time.Duration, 0, ticks)
	for n := 1; n <= ticks; n++ {
		p.Wait()
		out = append(out, time.Since(origin)-time.Duration(n)*period)
		busyFor(work)
	}
	return out
}

// The central claim of this repository: sleeping for a full period after each
// tick's work drifts by the duration of that work on every tick, while
// sleeping until an absolute deadline keeps lateness bounded.
//
// This asserts the shape of the error rather than any wall-clock figure, so it
// holds regardless of the host's timer granularity. Drift is unbounded growth
// proportional to tick count; a correct pacer's lateness stays within a small
// constant no matter how long the loop runs.
func TestSleepDeltaDriftsAndAbsoluteDeadlineDoesNot(t *testing.T) {
	if testing.Short() {
		t.Skip("timing-dependent; skipped under -short")
	}

	const (
		period = 10 * time.Millisecond
		work   = 5 * time.Millisecond
		ticks  = 40
	)

	// If a pacer drifts by the work duration each tick, by the final tick it is
	// late by roughly ticks*work. Half that is a threshold no bounded pacer
	// reaches and every drifting one passes.
	driftFloor := time.Duration(0.5 * float64(ticks) * float64(work))

	// A bounded pacer's lateness is a function of timer granularity, not of
	// tick count. A fifth of the drift budget leaves room for a coarse timer
	// while staying far below anything that accumulates.
	boundedCeiling := time.Duration(0.2 * float64(ticks) * float64(work))

	drifting := lateness(t, SleepDelta, period, work, ticks)
	if last := drifting[len(drifting)-1]; last < driftFloor {
		t.Errorf("sleep-delta final lateness %v, expected to have drifted past %v", last, driftFloor)
	}

	steady := lateness(t, AbsoluteDeadline, period, work, ticks)
	var worst time.Duration
	for _, l := range steady {
		if l > worst {
			worst = l
		}
	}
	if worst > boundedCeiling {
		t.Errorf("absolute-deadline worst lateness %v exceeds %v, so it is not holding a bound", worst, boundedCeiling)
	}

	// Growth, not magnitude, is what separates the two. Compare the first
	// quarter of the run against the last.
	q := ticks / 4
	growth := func(s []time.Duration) time.Duration {
		var head, tail time.Duration
		for i := 0; i < q; i++ {
			head += s[i]
			tail += s[len(s)-1-i]
		}
		return (tail - head) / time.Duration(q)
	}
	if growth(steady) >= growth(drifting) {
		t.Errorf("lateness grew as fast for absolute-deadline (%v) as for sleep-delta (%v); the strategies are not behaving differently",
			growth(steady), growth(drifting))
	}
}

// A deadline-based pacer whose loop has already overrun must return at once,
// so that the overrun is reported as lateness rather than absorbed by a sleep.
func TestAbsoluteDeadlineReturnsImmediatelyWhenBehind(t *testing.T) {
	if testing.Short() {
		t.Skip("timing-dependent; skipped under -short")
	}

	const period = 5 * time.Millisecond

	// Place the origin far enough in the past that the first several deadlines
	// are already due.
	origin := time.Now().Add(-1 * time.Second)
	p, err := New(AbsoluteDeadline, period, origin, DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	defer p.Stop()

	start := time.Now()
	for i := 0; i < 10; i++ {
		p.Wait()
	}
	if took := time.Since(start); took > 2*time.Millisecond {
		t.Errorf("10 already-due ticks took %v, want near zero", took)
	}
}

// Spin-tail must not return before its deadline; that is the whole point of
// the busy-wait.
func TestSpinTailDoesNotReturnEarly(t *testing.T) {
	if testing.Short() {
		t.Skip("timing-dependent; skipped under -short")
	}

	const period = 10 * time.Millisecond

	origin := time.Now()
	p, err := New(SpinTail, period, origin, Options{SpinSlack: 2 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Stop()

	for n := 1; n <= 10; n++ {
		p.Wait()
		deadline := origin.Add(time.Duration(n) * period)
		if time.Now().Before(deadline) {
			t.Fatalf("tick %d returned before its deadline", n)
		}
	}
}

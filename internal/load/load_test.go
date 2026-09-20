package load

import (
	"runtime"
	"testing"
	"time"
)

func TestSpinBurnsAtLeastTheRequestedTime(t *testing.T) {
	const want = 20 * time.Millisecond

	start := time.Now()
	Spin(want)
	got := time.Since(start)

	if got < want {
		t.Errorf("Spin(%v) returned after %v, must not return early", want, got)
	}
	// A spin should overshoot by the cost of one clock read, not by a timer
	// quantum. A generous ceiling still catches an accidental sleep.
	if got > 5*want {
		t.Errorf("Spin(%v) took %v, far longer than requested", want, got)
	}
}

func TestSpinOnNonPositiveDurationReturnsImmediately(t *testing.T) {
	for _, d := range []time.Duration{0, -time.Second} {
		start := time.Now()
		Spin(d)
		if got := time.Since(start); got > 5*time.Millisecond {
			t.Errorf("Spin(%v) took %v, want immediate return", d, got)
		}
	}
}

// The generators are judged on lifecycle, not on how much CPU they burn:
// asserting a throughput figure would be a flaky test of the host rather than
// of this code. What must hold is that stop always returns.
func TestContentionStartsAndStops(t *testing.T) {
	stop := Contention(runtime.NumCPU())
	time.Sleep(10 * time.Millisecond)

	done := make(chan struct{})
	go func() { stop(); close(done) }()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Contention stop() did not return; a worker is not observing the done channel")
	}
}

func TestContentionWithNoWorkers(t *testing.T) {
	stop := Contention(0)
	stop() // must not block or panic with nothing to wait for
}

func TestChurnStartsAndStops(t *testing.T) {
	stop := Churn(32 << 10)
	time.Sleep(10 * time.Millisecond)

	done := make(chan struct{})
	go func() { stop(); close(done) }()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Churn stop() did not return")
	}
}

func TestChurnDefaultsNonPositiveAllocSize(t *testing.T) {
	stop := Churn(0)
	stop()
}

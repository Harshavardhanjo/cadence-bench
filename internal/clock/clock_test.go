package clock

import (
	"testing"
	"time"

	"github.com/Harshavardhanjo/cadence-bench/internal/stats"
)

// Probing is slow on hosts with a coarse timer, so the suite shares one
// measurement rather than re-probing per test.
var probed = Measure(25)

func TestMeasurePopulatesEveryCharacteristic(t *testing.T) {
	if probed.Resolution.Count == 0 {
		t.Error("Resolution has no samples")
	}
	if probed.SleepGranularity.Count == 0 {
		t.Error("SleepGranularity has no samples")
	}
	if probed.ClockReadCost.Count == 0 {
		t.Error("ClockReadCost has no samples")
	}
	if probed.RequestedSleep != time.Millisecond {
		t.Errorf("RequestedSleep = %v, want 1ms", probed.RequestedSleep)
	}
}

// A sleep cannot return before the duration it was asked for. If this fails the
// harness is measuring something other than what it claims.
func TestSleepGranularityIsAtLeastRequested(t *testing.T) {
	requested := float64(probed.RequestedSleep.Nanoseconds())
	if probed.SleepGranularity.Min < requested {
		t.Errorf("minimum observed sleep %.0fns is below the %.0fns requested",
			probed.SleepGranularity.Min, requested)
	}
}

// Resolution is defined as the smallest non-zero advance, so zero would mean
// the spin loop recorded a step it should have rejected.
func TestResolutionIsPositive(t *testing.T) {
	if probed.Resolution.Min <= 0 {
		t.Errorf("Resolution.Min = %v, want positive", probed.Resolution.Min)
	}
}

// The read-cost batch must be large enough to exceed the clock's own
// resolution, or the measurement collapses to zero. This is the bug the batch
// size exists to avoid.
func TestClockReadCostIsPositive(t *testing.T) {
	if probed.ClockReadCost.P50 <= 0 {
		t.Errorf("ClockReadCost.P50 = %v, want positive; batch of %d reads was too small to measure against a %.0fns clock resolution",
			probed.ClockReadCost.P50, readBatch, probed.Resolution.P50)
	}
}

func TestMeasureDefaultsNonPositiveSampleCount(t *testing.T) {
	// Exercises the defaulting branch only; the shared probe covers the rest.
	if got := Measure(-1); got.Resolution.Count == 0 {
		t.Error("Measure(-1) produced no samples, want a default count")
	}
}

func TestCheckAdequateRejectsPeriodsTheClockCannotResolve(t *testing.T) {
	// A clock advancing in 1ms steps cannot resolve a tenth of a 1ms period.
	coarse := Characteristics{Resolution: stats.Summary{P50: float64(time.Millisecond)}}

	if err := coarse.CheckAdequate(time.Millisecond); err == nil {
		t.Error("CheckAdequate accepted a 1ms period against a 1ms clock resolution")
	}
	if err := coarse.CheckAdequate(20 * time.Millisecond); err != nil {
		t.Errorf("CheckAdequate rejected a 20ms period against a 1ms resolution: %v", err)
	}
}

func TestMinimumResolvablePeriodIsTenQuanta(t *testing.T) {
	c := Characteristics{Resolution: stats.Summary{P50: 1000}}

	if got, want := c.MinimumResolvablePeriod(), 10*time.Microsecond; got != want {
		t.Errorf("MinimumResolvablePeriod = %v, want %v", got, want)
	}
}

func TestSleepCanHoldComparesAgainstPeriod(t *testing.T) {
	coarse := Characteristics{SleepGranularity: stats.Summary{P50: float64(15600 * time.Microsecond)}}

	if coarse.SleepCanHold(20*time.Millisecond) != true {
		t.Error("a 15.6ms quantum should be reported as able to hold a 20ms period")
	}
	if coarse.SleepCanHold(10*time.Millisecond) != false {
		t.Error("a 15.6ms quantum cannot hold a 10ms period")
	}
}

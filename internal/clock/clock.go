// Package clock characterises the host's timing primitives.
//
// Two platform limits bound everything this harness reports, and both have to
// be published alongside any result.
//
// The first is sleep granularity. Windows' default timer quantum is 15.6ms, and
// a Go runtime that does not request a finer resolution cannot sleep for less
// than that, which makes a 20ms cadence unholdable by sleeping however the loop
// is written. Attributing that to a pacing strategy would be wrong.
//
// The second is clock resolution, and it is the more dangerous of the two
// because it fails silently. If the monotonic clock advances in steps of half a
// millisecond, then every lateness measurement is quantised to half a
// millisecond, and a table of sub-millisecond percentiles computed from it is
// invented precision. Measure detects this so the caller can refuse to report
// figures the clock cannot support.
package clock

import (
	"fmt"
	"sync/atomic"
	"time"

	"github.com/Harshavardhanjo/cadence-bench/internal/stats"
)

// readSink absorbs the clock readings taken while timing them. Discarding them
// with a blank assignment is not enough: time.Now is inlined to a runtime
// intrinsic, so a read whose result goes unused is eliminated outright and the
// batch appears to take no time at all.
var readSink int64

// Characteristics describes the host's timing primitives. All distributions are
// in nanoseconds.
type Characteristics struct {
	// Resolution is the distribution of the smallest non-zero advance observed
	// between consecutive monotonic clock reads. This is the quantum every
	// other measurement in the harness is rounded to.
	Resolution stats.Summary `json:"resolution_ns"`

	// SleepGranularity is the distribution of actual durations observed when
	// asking for the shortest useful sleep. Its median is effectively the
	// platform's timer quantum: a deadline nearer than one quantum can only be
	// hit by spinning.
	SleepGranularity stats.Summary `json:"sleep_granularity_ns"`

	// ClockReadCost is the amortised cost of one monotonic clock read.
	// Spin-tail pays it on every iteration of its busy-wait, so it sets the
	// floor on how precisely a spin can land on a deadline.
	ClockReadCost stats.Summary `json:"clock_read_cost_ns"`

	// RequestedSleep is what SleepGranularity asked for, for context.
	RequestedSleep time.Duration `json:"requested_sleep_ns"`
}

// readBatch is the number of clock reads timed as a unit. It has to be large
// enough that the batch takes appreciably longer than the clock's own
// resolution: at a few nanoseconds per read and a resolution of half a
// millisecond, a batch of a thousand reads measures as taking zero time.
const readBatch = 200000

// Measure probes the host. samples controls how many observations each figure
// is drawn from; a few hundred shows the quantum clearly.
//
// Cost is dominated by the sleep probe, which takes samples multiplied by the
// host's timer quantum: 200 samples on a host with a 15.6ms quantum is about
// three seconds.
func Measure(samples int) Characteristics {
	if samples <= 0 {
		samples = 200
	}

	// 1ms is below every common timer quantum, so what comes back is the floor
	// the platform can resolve rather than the duration requested.
	const requested = time.Millisecond

	return Characteristics{
		Resolution:       measureResolution(samples),
		SleepGranularity: measureSleep(requested, samples),
		ClockReadCost:    measureReadCost(samples),
		RequestedSleep:   requested,
	}
}

// measureResolution spins on the clock until it advances, recording the size of
// each step. The smallest non-zero difference between two reads is the clock's
// resolution by definition.
func measureResolution(samples int) stats.Summary {
	deltas := make([]float64, 0, samples)

	for len(deltas) < samples {
		a := time.Now()
		for {
			if d := time.Since(a); d > 0 {
				deltas = append(deltas, float64(d.Nanoseconds()))
				break
			}
		}
	}
	return stats.Summarize(deltas)
}

func measureSleep(requested time.Duration, samples int) stats.Summary {
	observed := make([]float64, 0, samples)

	for i := 0; i < samples; i++ {
		start := time.Now()
		time.Sleep(requested)
		observed = append(observed, float64(time.Since(start).Nanoseconds()))
	}
	return stats.Summarize(observed)
}

func measureReadCost(samples int) stats.Summary {
	costs := make([]float64, 0, samples)

	for i := 0; i < samples; i++ {
		var acc int64
		start := time.Now()
		for j := 0; j < readBatch; j++ {
			acc += time.Now().UnixNano()
		}
		elapsed := time.Since(start).Nanoseconds()

		atomic.AddInt64(&readSink, acc)
		costs = append(costs, float64(elapsed)/readBatch)
	}
	return stats.Summarize(costs)
}

// MinimumResolvablePeriod returns the shortest period whose lateness this host
// can measure to a tenth of a period, which is the coarsest quantisation at
// which percentile comparisons between strategies mean anything.
func (c Characteristics) MinimumResolvablePeriod() time.Duration {
	return time.Duration(c.Resolution.P50 * 10)
}

// CheckAdequate reports whether the host can support measurements at the given
// period, and returns an error describing the problem when it cannot.
//
// Callers are expected to surface this rather than swallow it. A harness that
// prints a p99 in microseconds from a clock that advances in half-millisecond
// steps is worse than one that refuses: the numbers look authoritative and are
// entirely an artefact of the platform.
func (c Characteristics) CheckAdequate(period time.Duration) error {
	if min := c.MinimumResolvablePeriod(); period < min {
		return fmt.Errorf(
			"clock resolution is %.0fns, which quantises every measurement; a period of %v needs at least %v to be resolvable to a tenth of a period",
			c.Resolution.P50, period, min)
	}
	return nil
}

// SleepCanHold reports whether a sleep-based strategy could hit a deadline at
// the given period on this host at all, ignoring load.
//
// When this is false, every sleeping strategy is limited by the platform rather
// than by its own logic, and only spin-tail can hold the cadence. That is a
// property of the OS and runtime version, not a finding about the strategies.
func (c Characteristics) SleepCanHold(period time.Duration) bool {
	return c.SleepGranularity.P50 < float64(period)
}

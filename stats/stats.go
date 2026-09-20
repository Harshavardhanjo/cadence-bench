// Package stats summarises series of latency measurements.
//
// The package is unit-agnostic: callers pass float64 samples and get results
// back in the same unit. Throughout cadence-bench those samples are
// nanoseconds, kept as float64 so that means and standard deviations do not
// need a separate integer/float path.
//
// It is exported rather than internal because the sibling jitter-bench measures
// the receive side of the same audio path and needs the identical percentile
// definitions. Two benchmarks that report percentiles computed differently
// cannot be read side by side, which is the whole reason for sharing this
// rather than copying it.
package stats

import (
	"math"
	"sort"
)

// Summary describes the distribution of a sample series.
//
// Percentiles rather than a mean are the primary output because a paced audio
// loop is a deadline system: a loop that hits 20ms on average but misses by
// 15ms once per second produces audible artefacts, and the mean hides that
// entirely.
type Summary struct {
	Count  int     `json:"count"`
	Min    float64 `json:"min"`
	Max    float64 `json:"max"`
	Mean   float64 `json:"mean"`
	StdDev float64 `json:"stddev"`
	P50    float64 `json:"p50"`
	P90    float64 `json:"p90"`
	P99    float64 `json:"p99"`
	P999   float64 `json:"p999"`
}

// Summarize computes the distribution of samples.
//
// It sorts a copy rather than the caller's slice: the scenario runner reuses
// one preallocated buffer across runs and relies on sample order being
// preserved for CSV export.
func Summarize(samples []float64) Summary {
	if len(samples) == 0 {
		return Summary{}
	}

	sorted := make([]float64, len(samples))
	copy(sorted, samples)
	sort.Float64s(sorted)

	var sum float64
	for _, v := range sorted {
		sum += v
	}
	mean := sum / float64(len(sorted))

	// Sample (n-1) standard deviation, which is the convention for reporting
	// spread over a finite set of observations drawn from a longer-running
	// process. With a single sample the spread is undefined, reported as 0.
	stddev := 0.0
	if len(sorted) > 1 {
		var sumSq float64
		for _, v := range sorted {
			d := v - mean
			sumSq += d * d
		}
		stddev = math.Sqrt(sumSq / float64(len(sorted)-1))
	}

	return Summary{
		Count:  len(sorted),
		Min:    sorted[0],
		Max:    sorted[len(sorted)-1],
		Mean:   mean,
		StdDev: stddev,
		P50:    Percentile(sorted, 0.50),
		P90:    Percentile(sorted, 0.90),
		P99:    Percentile(sorted, 0.99),
		P999:   Percentile(sorted, 0.999),
	}
}

// Percentile returns the p-th percentile (p in [0,1]) of an already-sorted
// slice using the nearest-rank method: the smallest sample whose rank is at
// least p*N.
//
// Nearest-rank is used instead of linear interpolation because every reported
// figure is then a latency that the loop actually exhibited. An interpolated
// p99 is a number no tick ever recorded, which is the wrong thing to put in a
// table that readers will compare against their own measurements.
//
// Passing an unsorted slice yields a meaningless result; this is unexported
// behaviour relied on only by Summarize and the tests.
func Percentile(sorted []float64, p float64) float64 {
	n := len(sorted)
	if n == 0 {
		return 0
	}
	if p <= 0 {
		return sorted[0]
	}
	if p >= 1 {
		return sorted[n-1]
	}

	rank := int(math.Ceil(p * float64(n)))
	if rank < 1 {
		rank = 1
	}
	if rank > n {
		rank = n
	}
	return sorted[rank-1]
}

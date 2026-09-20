package stats

import (
	"math"
	"testing"
)

// ranks returns 1..n as float64 samples, already sorted.
func ranks(n int) []float64 {
	s := make([]float64, n)
	for i := range s {
		s[i] = float64(i + 1)
	}
	return s
}

func TestPercentileNearestRank(t *testing.T) {
	s := ranks(100)

	cases := []struct {
		p    float64
		want float64
	}{
		{0.50, 50},   // ceil(50) -> index 49
		{0.90, 90},   // ceil(90) -> index 89
		{0.99, 99},   // ceil(99) -> index 98
		{0.999, 100}, // ceil(99.9) -> index 99
		{0.0, 1},     // clamped to minimum
		{1.0, 100},   // clamped to maximum
	}
	for _, c := range cases {
		if got := Percentile(s, c.p); got != c.want {
			t.Errorf("Percentile(p=%v) = %v, want %v", c.p, got, c.want)
		}
	}
}

// Every reported percentile must be a value that actually occurred, which is
// the property that rules out interpolation.
func TestPercentileReturnsAnObservedSample(t *testing.T) {
	s := []float64{10, 20, 30, 40, 50}
	observed := map[float64]bool{10: true, 20: true, 30: true, 40: true, 50: true}

	for _, p := range []float64{0.1, 0.25, 0.5, 0.75, 0.9, 0.95, 0.99} {
		got := Percentile(s, p)
		if !observed[got] {
			t.Errorf("Percentile(p=%v) = %v, which is not one of the input samples", p, got)
		}
	}
}

func TestPercentileDegenerateInputs(t *testing.T) {
	if got := Percentile(nil, 0.5); got != 0 {
		t.Errorf("Percentile(nil) = %v, want 0", got)
	}
	if got := Percentile([]float64{7}, 0.99); got != 7 {
		t.Errorf("Percentile(single) = %v, want 7", got)
	}
}

func TestSummarize(t *testing.T) {
	got := Summarize([]float64{4, 1, 3, 2})

	if got.Count != 4 {
		t.Errorf("Count = %v, want 4", got.Count)
	}
	if got.Min != 1 || got.Max != 4 {
		t.Errorf("Min/Max = %v/%v, want 1/4", got.Min, got.Max)
	}
	if got.Mean != 2.5 {
		t.Errorf("Mean = %v, want 2.5", got.Mean)
	}
	// Sample stddev of {1,2,3,4} is sqrt(5/3).
	if want := math.Sqrt(5.0 / 3.0); math.Abs(got.StdDev-want) > 1e-12 {
		t.Errorf("StdDev = %v, want %v", got.StdDev, want)
	}
	if got.P50 != 2 {
		t.Errorf("P50 = %v, want 2", got.P50)
	}
}

// The scenario runner reuses one sample buffer and exports it as a
// chronological CSV, so Summarize must not reorder what it is given.
func TestSummarizeDoesNotMutateInput(t *testing.T) {
	in := []float64{4, 1, 3, 2}
	Summarize(in)

	for i, want := range []float64{4, 1, 3, 2} {
		if in[i] != want {
			t.Fatalf("input was reordered: got %v, want [4 1 3 2]", in)
		}
	}
}

func TestSummarizeEmpty(t *testing.T) {
	if got := Summarize(nil); got.Count != 0 {
		t.Errorf("Summarize(nil).Count = %v, want 0", got.Count)
	}
}

func TestSummarizeSingleSampleHasZeroSpread(t *testing.T) {
	got := Summarize([]float64{42})
	if got.StdDev != 0 {
		t.Errorf("StdDev = %v, want 0 for a single sample", got.StdDev)
	}
	if got.Mean != 42 || got.P99 != 42 {
		t.Errorf("Mean/P99 = %v/%v, want 42/42", got.Mean, got.P99)
	}
}

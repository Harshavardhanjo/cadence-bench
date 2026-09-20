// Package run executes pacing measurements and aggregates them.
//
// Every figure is produced from repeated runs rather than one. A single run of
// a timing benchmark reports the state of the machine at that moment: a
// background process, a thermal event or one unlucky collection moves a p99 by
// more than the difference between the strategies being compared. Reporting
// the spread across repetitions is what makes a difference in the table
// trustworthy.
package run

import (
	"fmt"
	"runtime"
	"time"

	"github.com/Harshavardhanjo/cadence-bench/internal/load"
	"github.com/Harshavardhanjo/cadence-bench/internal/pacer"
	"github.com/Harshavardhanjo/cadence-bench/internal/stats"
)

// Scenario is the background pressure a measurement runs under.
type Scenario struct {
	Name string `json:"name"`

	// ContentionProcs is the number of CPU-consuming goroutines competing with
	// the paced loop.
	ContentionProcs int `json:"contention_procs"`

	// ChurnBytes is the size of each allocation made by the garbage generator,
	// or zero to disable it.
	ChurnBytes int `json:"churn_bytes"`
}

// Scenarios returns the default set, ordered from least to most adversarial.
func Scenarios(numCPU int) []Scenario {
	return []Scenario{
		{Name: "idle"},
		{Name: "cpu-contention", ContentionProcs: numCPU},
		{Name: "gc-churn", ChurnBytes: 64 << 10},
		{Name: "contention+churn", ContentionProcs: numCPU, ChurnBytes: 64 << 10},
	}
}

// Config describes one measurement.
type Config struct {
	Kind     pacer.Kind    `json:"kind"`
	Period   time.Duration `json:"period_ns"`
	Work     time.Duration `json:"work_ns"`
	Ticks    int           `json:"ticks"`
	Warmup   int           `json:"warmup_ticks"`
	Scenario Scenario      `json:"scenario"`
	Opts     pacer.Options `json:"options"`
}

func (c Config) validate() error {
	if c.Period <= 0 {
		return fmt.Errorf("run: period must be positive, got %v", c.Period)
	}
	if c.Ticks <= 0 {
		return fmt.Errorf("run: ticks must be positive, got %d", c.Ticks)
	}
	if c.Warmup < 0 {
		return fmt.Errorf("run: warmup must not be negative, got %d", c.Warmup)
	}
	if c.Work < 0 {
		return fmt.Errorf("run: work must not be negative, got %v", c.Work)
	}
	return nil
}

// Rep is one repetition of a measurement.
//
// Lateness is the primary result and interval is the secondary one. They
// answer different questions, and a loop can be good at one and bad at the
// other: a loop whose period is slightly wrong has near-perfect intervals
// while drifting away from the wall clock without bound, and a loop that
// alternates early and late ticks holds its average rate while sounding
// broken.
type Rep struct {
	// Lateness is how far past its nominal deadline each tick woke, in
	// nanoseconds. Negative values mean the tick woke early, which only
	// spin-tail should manage.
	Lateness stats.Summary `json:"lateness_ns"`

	// Interval is the gap between consecutive wakes, in nanoseconds.
	Interval stats.Summary `json:"interval_ns"`

	// MissedDeadlines counts ticks that woke more than one full period late,
	// meaning the next deadline was already due when this one was serviced. In
	// an audio path that is the point at which a frame is not merely late but
	// lost.
	MissedDeadlines int `json:"missed_deadlines"`

	// MissRate is MissedDeadlines over ticks measured.
	MissRate float64 `json:"miss_rate"`

	// DriftPerTick is the average nanoseconds of lateness accumulated per tick
	// from the first measured tick to the last. A pacer that holds its rate
	// sits near zero however bad its jitter; a drifting one grows without
	// bound, which is a different and worse failure.
	DriftPerTick float64 `json:"drift_per_tick_ns"`

	// FinalLateness is the lateness of the last measured tick.
	FinalLateness float64 `json:"final_lateness_ns"`

	// Elapsed is the wall time of the whole run including warmup.
	Elapsed time.Duration `json:"elapsed_ns"`

	// Raw per-tick series in chronological order, for CSV export. Unexported
	// so JSON reports stay readable: four strategies across four scenarios at
	// 10,000 ticks would otherwise dominate the file.
	lateness []float64
	interval []float64
}

// Samples returns the raw per-tick lateness and interval series in
// chronological order, both in nanoseconds.
func (r *Rep) Samples() (lateness, interval []float64) {
	return r.lateness, r.interval
}

// Once performs a single repetition.
func Once(cfg Config) (*Rep, error) {
	if err := cfg.validate(); err != nil {
		return nil, err
	}

	var stops []func()
	if cfg.Scenario.ContentionProcs > 0 {
		stops = append(stops, load.Contention(cfg.Scenario.ContentionProcs))
	}
	if cfg.Scenario.ChurnBytes > 0 {
		stops = append(stops, load.Churn(cfg.Scenario.ChurnBytes))
	}
	defer func() {
		for _, stop := range stops {
			stop()
		}
	}()

	// Give the background load time to be scheduled, so the first measured
	// ticks are not the only quiet ones in the run.
	if len(stops) > 0 {
		time.Sleep(50 * time.Millisecond)
	}

	total := cfg.Warmup + cfg.Ticks

	// Preallocated so the measurement loop itself does not allocate. Growing a
	// slice mid-run would put collector work inside the ticks being timed and
	// attribute it to the pacer.
	lateness := make([]float64, 0, cfg.Ticks)
	interval := make([]float64, 0, cfg.Ticks)

	origin := time.Now()
	p, err := pacer.New(cfg.Kind, cfg.Period, origin, cfg.Opts)
	if err != nil {
		return nil, err
	}
	defer p.Stop()

	prev := origin
	for n := 1; n <= total; n++ {
		p.Wait()
		now := time.Now()

		if n > cfg.Warmup {
			nominal := time.Duration(n) * cfg.Period
			lateness = append(lateness, float64(now.Sub(origin)-nominal))
			interval = append(interval, float64(now.Sub(prev)))
		}
		prev = now

		load.Spin(cfg.Work)
	}
	elapsed := time.Since(origin)

	rep := &Rep{
		Lateness: stats.Summarize(lateness),
		Interval: stats.Summarize(interval),
		Elapsed:  elapsed,
		lateness: lateness,
		interval: interval,
	}

	budget := float64(cfg.Period)
	for _, l := range lateness {
		if l > budget {
			rep.MissedDeadlines++
		}
	}
	if len(lateness) > 0 {
		rep.MissRate = float64(rep.MissedDeadlines) / float64(len(lateness))
		rep.FinalLateness = lateness[len(lateness)-1]
	}
	if len(lateness) > 1 {
		rep.DriftPerTick = (lateness[len(lateness)-1] - lateness[0]) / float64(len(lateness)-1)
	}

	return rep, nil
}

// Spread is the distribution of one figure across repetitions.
//
// Min and Max are the honest error bar: the range actually observed, not a
// confidence interval derived from an assumption about the shape of the
// distribution. Timing tails are not normal, so a standard error computed as
// if they were would understate them.
type Spread struct {
	Median float64 `json:"median"`
	Min    float64 `json:"min"`
	Max    float64 `json:"max"`
}

func spreadOf(values []float64) Spread {
	if len(values) == 0 {
		return Spread{}
	}
	s := stats.Summarize(values)
	return Spread{Median: s.P50, Min: s.Min, Max: s.Max}
}

// Series is the aggregate of repeated measurements of one configuration.
type Series struct {
	Config Config `json:"config"`
	Reps   []*Rep `json:"reps"`

	// Cross-repetition spread of the headline figures, in nanoseconds except
	// MissRate which is a fraction.
	MedianLateness Spread `json:"median_lateness_ns"`
	P99Lateness    Spread `json:"p99_lateness_ns"`
	MaxLateness    Spread `json:"max_lateness_ns"`
	DriftPerTick   Spread `json:"drift_per_tick_ns"`
	MissRate       Spread `json:"miss_rate"`
}

// Repeat runs cfg reps times and aggregates the results.
func Repeat(cfg Config, reps int) (*Series, error) {
	if reps <= 0 {
		return nil, fmt.Errorf("run: reps must be positive, got %d", reps)
	}

	out := &Series{Config: cfg, Reps: make([]*Rep, 0, reps)}

	med := make([]float64, 0, reps)
	p99 := make([]float64, 0, reps)
	worst := make([]float64, 0, reps)
	drift := make([]float64, 0, reps)
	miss := make([]float64, 0, reps)

	for i := 0; i < reps; i++ {
		rep, err := Once(cfg)
		if err != nil {
			return nil, fmt.Errorf("run: repetition %d of %d: %w", i+1, reps, err)
		}
		out.Reps = append(out.Reps, rep)

		med = append(med, rep.Lateness.P50)
		p99 = append(p99, rep.Lateness.P99)
		worst = append(worst, rep.Lateness.Max)
		drift = append(drift, rep.DriftPerTick)
		miss = append(miss, rep.MissRate)
	}

	out.MedianLateness = spreadOf(med)
	out.P99Lateness = spreadOf(p99)
	out.MaxLateness = spreadOf(worst)
	out.DriftPerTick = spreadOf(drift)
	out.MissRate = spreadOf(miss)

	return out, nil
}

// Host records the machine a report was produced on.
//
// Results are not comparable across hosts, so a report without this is not
// interpretable. Timer granularity in particular is a property of the OS and
// the runtime version rather than of any strategy.
type Host struct {
	GOOS       string `json:"goos"`
	GOARCH     string `json:"goarch"`
	GoVersion  string `json:"go_version"`
	NumCPU     int    `json:"num_cpu"`
	GOMAXPROCS int    `json:"gomaxprocs"`
}

// DescribeHost captures the current machine.
func DescribeHost() Host {
	return Host{
		GOOS:       runtime.GOOS,
		GOARCH:     runtime.GOARCH,
		GoVersion:  runtime.Version(),
		NumCPU:     runtime.NumCPU(),
		GOMAXPROCS: runtime.GOMAXPROCS(0),
	}
}

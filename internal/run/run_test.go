package run

import (
	"testing"
	"time"

	"github.com/Harshavardhanjo/cadence-bench/internal/pacer"
)

func baseConfig() Config {
	return Config{
		Kind:     pacer.AbsoluteDeadline,
		Period:   time.Millisecond,
		Work:     0,
		Ticks:    10,
		Warmup:   3,
		Scenario: Scenario{Name: "idle"},
		Opts:     pacer.DefaultOptions(),
	}
}

func TestConfigValidation(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*Config)
	}{
		{"zero period", func(c *Config) { c.Period = 0 }},
		{"zero ticks", func(c *Config) { c.Ticks = 0 }},
		{"negative warmup", func(c *Config) { c.Warmup = -1 }},
		{"negative work", func(c *Config) { c.Work = -time.Millisecond }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cfg := baseConfig()
			c.mutate(&cfg)
			if _, err := Once(cfg); err == nil {
				t.Fatalf("Once with %s succeeded, want error", c.name)
			}
		})
	}
}

func TestOnceUnknownPacerIsReported(t *testing.T) {
	cfg := baseConfig()
	cfg.Kind = pacer.Kind("no-such-strategy")
	if _, err := Once(cfg); err == nil {
		t.Fatal("Once with an unknown strategy succeeded, want error")
	}
}

// Warmup ticks must be excluded from the results, not merely run. The startup
// transient is exactly where the worst samples live, so counting them would
// make every strategy look bad in proportion to how short the run is.
func TestOnceDiscardsWarmupTicks(t *testing.T) {
	cfg := baseConfig()

	rep, err := Once(cfg)
	if err != nil {
		t.Fatal(err)
	}

	if rep.Lateness.Count != cfg.Ticks {
		t.Errorf("Lateness.Count = %d, want %d (warmup must be discarded)", rep.Lateness.Count, cfg.Ticks)
	}
	lateness, interval := rep.Samples()
	if len(lateness) != cfg.Ticks || len(interval) != cfg.Ticks {
		t.Errorf("raw series lengths = %d/%d, want %d", len(lateness), len(interval), cfg.Ticks)
	}
}

func TestOnceReportsMissRateConsistently(t *testing.T) {
	rep, err := Once(baseConfig())
	if err != nil {
		t.Fatal(err)
	}

	if rep.MissedDeadlines < 0 || rep.MissedDeadlines > rep.Lateness.Count {
		t.Errorf("MissedDeadlines = %d, outside 0..%d", rep.MissedDeadlines, rep.Lateness.Count)
	}
	want := float64(rep.MissedDeadlines) / float64(rep.Lateness.Count)
	if rep.MissRate != want {
		t.Errorf("MissRate = %v, want %v", rep.MissRate, want)
	}
}

func TestRepeatRejectsNonPositiveReps(t *testing.T) {
	for _, reps := range []int{0, -1} {
		if _, err := Repeat(baseConfig(), reps); err == nil {
			t.Errorf("Repeat(reps=%d) succeeded, want error", reps)
		}
	}
}

func TestRepeatAggregatesEveryRepetition(t *testing.T) {
	const reps = 3

	series, err := Repeat(baseConfig(), reps)
	if err != nil {
		t.Fatal(err)
	}

	if len(series.Reps) != reps {
		t.Fatalf("len(Reps) = %d, want %d", len(series.Reps), reps)
	}
	// The spread must bracket the median, or the error bars are wrong.
	s := series.P99Lateness
	if s.Min > s.Median || s.Median > s.Max {
		t.Errorf("P99 spread is not ordered: min=%v median=%v max=%v", s.Min, s.Median, s.Max)
	}
}

func TestScenariosAreDistinctAndOrdered(t *testing.T) {
	got := Scenarios(4)
	if len(got) == 0 {
		t.Fatal("Scenarios returned nothing")
	}

	seen := map[string]bool{}
	for _, s := range got {
		if seen[s.Name] {
			t.Errorf("duplicate scenario name %q", s.Name)
		}
		seen[s.Name] = true
	}
	if got[0].ContentionProcs != 0 || got[0].ChurnBytes != 0 {
		t.Errorf("first scenario should be unloaded, got %+v", got[0])
	}
}

func TestDescribeHostIsPopulated(t *testing.T) {
	h := DescribeHost()
	if h.GOOS == "" || h.GOARCH == "" || h.GoVersion == "" {
		t.Errorf("DescribeHost returned incomplete data: %+v", h)
	}
	if h.NumCPU < 1 || h.GOMAXPROCS < 1 {
		t.Errorf("DescribeHost CPU counts invalid: %+v", h)
	}
}

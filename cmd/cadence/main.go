// Command cadence measures how well a Go loop holds a fixed period.
//
// The motivating case is the send loop of a real-time audio path, which must
// hand a frame to the transport every 20ms. Four pacing strategies are compared
// under increasing background load, and the host's own timing limits are
// reported alongside so that a platform ceiling is not mistaken for a property
// of the code.
//
//	cadence probe              characterise this host's clock and timers
//	cadence run                compare every strategy across every scenario
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"math"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/Harshavardhanjo/cadence-bench/internal/clock"
	"github.com/Harshavardhanjo/cadence-bench/internal/pacer"
	"github.com/Harshavardhanjo/cadence-bench/internal/run"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}

	var err error
	switch os.Args[1] {
	case "probe":
		err = probeCmd(os.Args[2:])
	case "run":
		err = runCmd(os.Args[2:])
	case "help", "-h", "--help":
		usage()
		return
	default:
		fmt.Fprintf(os.Stderr, "cadence: unknown command %q\n\n", os.Args[1])
		usage()
		os.Exit(2)
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "cadence: %v\n", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `cadence measures how well a loop holds a fixed period.

usage:
  cadence probe [flags]   characterise this host's clock and timer resolution
  cadence run   [flags]   compare pacing strategies under load

Run a command with -h for its flags.
`)
}

// durNS renders a nanosecond figure at a scale a reader can compare at a
// glance. Sub-millisecond values are the interesting range for a 20ms period,
// so microseconds are the default unit rather than milliseconds.
func durNS(ns float64) string {
	switch a := math.Abs(ns); {
	case a >= 1e6:
		return fmt.Sprintf("%.2fms", ns/1e6)
	case a >= 1e3:
		return fmt.Sprintf("%.1fus", ns/1e3)
	default:
		return fmt.Sprintf("%.0fns", ns)
	}
}

func probeCmd(args []string) error {
	fs := flag.NewFlagSet("probe", flag.ExitOnError)
	samples := fs.Int("samples", 200, "observations per characteristic")
	asJSON := fs.Bool("json", false, "emit JSON instead of a table")
	period := fs.Duration("period", 20*time.Millisecond, "period to assess this host against")
	if err := fs.Parse(args); err != nil {
		return err
	}

	host := run.DescribeHost()
	c := clock.Measure(*samples)

	if *asJSON {
		return json.NewEncoder(os.Stdout).Encode(struct {
			Host  run.Host              `json:"host"`
			Clock clock.Characteristics `json:"clock"`
		}{host, c})
	}

	fmt.Printf("host      %s/%s, %s, %d cpu, GOMAXPROCS=%d\n",
		host.GOOS, host.GOARCH, host.GoVersion, host.NumCPU, host.GOMAXPROCS)
	fmt.Println()
	fmt.Printf("clock resolution      median %-10s  min %-10s  max %s\n",
		durNS(c.Resolution.P50), durNS(c.Resolution.Min), durNS(c.Resolution.Max))
	fmt.Printf("sleep granularity     median %-10s  min %-10s  max %s   (requested %v)\n",
		durNS(c.SleepGranularity.P50), durNS(c.SleepGranularity.Min), durNS(c.SleepGranularity.Max), c.RequestedSleep)
	fmt.Printf("clock read cost       median %-10s\n", durNS(c.ClockReadCost.P50))
	fmt.Println()

	fmt.Printf("assessed against a %v period:\n", *period)
	if err := c.CheckAdequate(*period); err != nil {
		fmt.Printf("  MEASUREMENT NOT VALID: %v\n", err)
	} else {
		fmt.Printf("  clock is fine: lateness resolves to %s, under a tenth of the period\n", durNS(c.Resolution.P50))
	}
	if c.SleepCanHold(*period) {
		fmt.Printf("  sleeping is viable: granularity %s is inside the period\n", durNS(c.SleepGranularity.P50))
	} else {
		fmt.Printf("  SLEEPING CANNOT HOLD THIS PERIOD: granularity %s exceeds the %v period,\n",
			durNS(c.SleepGranularity.P50), *period)
		fmt.Printf("  so every sleeping strategy is capped by the platform and only spin-tail can hold it\n")
	}
	return nil
}

// report is the full JSON artefact. Host and clock data are included because
// results are not comparable across machines without them.
type report struct {
	Host     run.Host              `json:"host"`
	Clock    clock.Characteristics `json:"clock"`
	Warnings []string              `json:"warnings"`
	Series   []*run.Series         `json:"series"`
}

func selectKinds(spec string) ([]pacer.Kind, error) {
	all := pacer.Kinds()
	if spec == "" || spec == "all" {
		return all, nil
	}

	valid := map[string]pacer.Kind{}
	for _, k := range all {
		valid[string(k)] = k
	}

	var out []pacer.Kind
	for _, name := range strings.Split(spec, ",") {
		name = strings.TrimSpace(name)
		k, ok := valid[name]
		if !ok {
			names := make([]string, 0, len(all))
			for _, k := range all {
				names = append(names, string(k))
			}
			return nil, fmt.Errorf("unknown strategy %q; known: %s", name, strings.Join(names, ", "))
		}
		out = append(out, k)
	}
	return out, nil
}

func selectScenarios(spec string, numCPU int) ([]run.Scenario, error) {
	all := run.Scenarios(numCPU)
	if spec == "" || spec == "all" {
		return all, nil
	}

	valid := map[string]run.Scenario{}
	for _, s := range all {
		valid[s.Name] = s
	}

	var out []run.Scenario
	for _, name := range strings.Split(spec, ",") {
		name = strings.TrimSpace(name)
		s, ok := valid[name]
		if !ok {
			names := make([]string, 0, len(all))
			for _, s := range all {
				names = append(names, s.Name)
			}
			return nil, fmt.Errorf("unknown scenario %q; known: %s", name, strings.Join(names, ", "))
		}
		out = append(out, s)
	}
	return out, nil
}

func runCmd(args []string) error {
	fs := flag.NewFlagSet("run", flag.ExitOnError)
	period := fs.Duration("period", 20*time.Millisecond, "target period; 20ms is one Opus frame at the usual framing")
	work := fs.Duration("work", 2*time.Millisecond, "simulated per-tick encode work, spun not slept")
	ticks := fs.Int("ticks", 250, "measured ticks per repetition")
	warmup := fs.Int("warmup", 50, "ticks discarded before measurement begins")
	reps := fs.Int("reps", 3, "repetitions per configuration; the spread across them is the error bar")
	spinSlack := fs.Duration("spin-slack", 2*time.Millisecond, "how early spin-tail stops sleeping and starts busy-waiting")
	strategies := fs.String("strategies", "all", "comma-separated strategies, or all")
	scenarios := fs.String("scenarios", "all", "comma-separated scenarios, or all")
	out := fs.String("out", "", "also write the full JSON report to this path")
	probeSamples := fs.Int("probe-samples", 200, "observations per host characteristic")
	force := fs.Bool("force", false, "measure even when this host's clock cannot resolve the period")
	if err := fs.Parse(args); err != nil {
		return err
	}

	kinds, err := selectKinds(*strategies)
	if err != nil {
		return err
	}
	scen, err := selectScenarios(*scenarios, runtime.NumCPU())
	if err != nil {
		return err
	}

	host := run.DescribeHost()
	fmt.Fprintf(os.Stderr, "probing host timing primitives...\n")
	characteristics := clock.Measure(*probeSamples)

	var warnings []string
	if err := characteristics.CheckAdequate(*period); err != nil {
		if !*force {
			return fmt.Errorf("%w\n\nthe reported percentiles would be an artefact of the clock rather than of the strategies.\nre-run with a longer -period, or with -force to measure anyway", err)
		}
		warnings = append(warnings, "clock resolution cannot resolve this period: "+err.Error())
	}
	if !characteristics.SleepCanHold(*period) {
		warnings = append(warnings, fmt.Sprintf(
			"sleep granularity %s exceeds the %v period, so every sleeping strategy is capped by the platform and only spin-tail can hold the cadence on this host",
			durNS(characteristics.SleepGranularity.P50), *period))
	}

	rep := &report{Host: host, Clock: characteristics, Warnings: warnings}

	total := len(kinds) * len(scen)
	done := 0
	for _, k := range kinds {
		for _, s := range scen {
			done++
			fmt.Fprintf(os.Stderr, "[%d/%d] %s under %s (%d reps x %d ticks)\n", done, total, k, s.Name, *reps, *ticks)

			series, err := run.Repeat(run.Config{
				Kind:     k,
				Period:   *period,
				Work:     *work,
				Ticks:    *ticks,
				Warmup:   *warmup,
				Scenario: s,
				Opts:     pacer.Options{SpinSlack: *spinSlack},
			}, *reps)
			if err != nil {
				return err
			}
			rep.Series = append(rep.Series, series)
		}
	}

	printTable(rep, *period, *work)

	if *out != "" {
		f, err := os.Create(*out)
		if err != nil {
			return err
		}
		defer f.Close()

		enc := json.NewEncoder(f)
		enc.SetIndent("", "  ")
		if err := enc.Encode(rep); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "\nwrote %s\n", *out)
	}
	return nil
}

func printTable(rep *report, period, work time.Duration) {
	fmt.Printf("\nperiod %v, work %v, %s/%s, %s, %d cpu\n",
		period, work, rep.Host.GOOS, rep.Host.GOARCH, rep.Host.GoVersion, rep.Host.NumCPU)
	fmt.Printf("clock resolution %s, sleep granularity %s\n",
		durNS(rep.Clock.Resolution.P50), durNS(rep.Clock.SleepGranularity.P50))

	for _, w := range rep.Warnings {
		fmt.Printf("\nWARNING: %s\n", w)
	}

	fmt.Printf("\nlateness past deadline, median of per-run figures with the range across runs\n\n")
	fmt.Printf("%-18s %-17s %10s %26s %11s %13s %7s\n",
		"strategy", "scenario", "median", "p99 (min..max)", "worst", "drift/tick", "missed")
	fmt.Printf("%-18s %-17s %10s %26s %11s %13s %7s\n",
		strings.Repeat("-", 18), strings.Repeat("-", 17), strings.Repeat("-", 10),
		strings.Repeat("-", 26), strings.Repeat("-", 11), strings.Repeat("-", 13), strings.Repeat("-", 7))

	for _, s := range rep.Series {
		p99 := fmt.Sprintf("%s (%s..%s)", durNS(s.P99Lateness.Median), durNS(s.P99Lateness.Min), durNS(s.P99Lateness.Max))
		fmt.Printf("%-18s %-17s %10s %26s %11s %13s %6.1f%%\n",
			s.Config.Kind,
			s.Config.Scenario.Name,
			durNS(s.MedianLateness.Median),
			p99,
			durNS(s.MaxLateness.Median),
			durNS(s.DriftPerTick.Median),
			s.MissRate.Median*100,
		)
	}

	fmt.Printf("\nmissed = ticks woken more than one full period late, so the next frame was already due\n")
	fmt.Printf("drift/tick = lateness accumulated per tick; near zero means the loop holds its rate\n")
}

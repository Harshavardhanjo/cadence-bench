//go:build js && wasm

// Command wasm exposes the probe, a single measurement and the shared
// percentile definitions to JavaScript.
//
// Unlike its siblings, cadence-bench is a measurement rather than a simulation,
// so running it in a browser measures the browser. That is the point of the
// page, not a compromise of it: the host is a single-threaded Go runtime with no
// asynchronous preemption, whose sleeps are setTimeout and whose clock is
// performance.now, and the same clock adequacy gate as the command line tool
// decides whether this host can say anything about a 20ms period at all.
//
// Both entry points return Promises. A Go function called from JavaScript may
// not block, and every figure here comes from sleeping, so the work runs on a
// goroutine and resolves the Promise when it finishes.
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"syscall/js"
	"time"

	"github.com/Harshavardhanjo/cadence-bench/internal/clock"
	"github.com/Harshavardhanjo/cadence-bench/internal/pacer"
	"github.com/Harshavardhanjo/cadence-bench/internal/run"
	"github.com/Harshavardhanjo/cadence-bench/stats"
)

// probeResult is the command line tool's probe output plus the two verdicts it
// prints, computed here so the page cannot word the gate differently from Go.
type probeResult struct {
	Host  run.Host              `json:"host"`
	Clock clock.Characteristics `json:"clock"`

	PeriodNs                  int64  `json:"periodNs"`
	MinimumResolvablePeriodNs int64  `json:"minimumResolvablePeriodNs"`
	AdequateError             string `json:"adequateError,omitempty"`
	SleepCanHold              bool   `json:"sleepCanHold"`
}

type probeRequest struct {
	Samples  int     `json:"samples"`
	PeriodMs float64 `json:"periodMs"`
}

// onceRequest carries durations in milliseconds, which is what a slider and a
// JSON number both handle naturally. They become time.Duration here, once.
type onceRequest struct {
	Kind        string  `json:"kind"`
	PeriodMs    float64 `json:"periodMs"`
	WorkMs      float64 `json:"workMs"`
	Ticks       int     `json:"ticks"`
	Warmup      int     `json:"warmup"`
	SpinSlackMs float64 `json:"spinSlackMs"`
}

// onceResult is one repetition with its raw per-tick series, which the command
// line tool keeps out of its JSON report but the page needs to draw drift.
type onceResult struct {
	Kind            string        `json:"kind"`
	Lateness        stats.Summary `json:"latenessNs"`
	Interval        stats.Summary `json:"intervalNs"`
	MissedDeadlines int           `json:"missedDeadlines"`
	MissRate        float64       `json:"missRate"`
	DriftPerTick    float64       `json:"driftPerTickNs"`
	FinalLateness   float64       `json:"finalLatenessNs"`
	ElapsedNs       int64         `json:"elapsedNs"`
	Samples         []float64     `json:"latenessSeriesNs"`
	Intervals       []float64     `json:"intervalSeriesNs"`
}

// summarizeRequest is a set of per-repetition figures to aggregate. The page
// reports the median across repetitions with the observed range, exactly as the
// command line tool does, and asks Go for it rather than reimplementing
// nearest-rank percentiles in TypeScript, where they would quietly disagree
// with the definitions the sibling benchmarks share.
type summarizeRequest struct {
	Values []float64 `json:"values"`
}

func summarize(req summarizeRequest) (any, error) {
	return stats.Summarize(req.Values), nil
}

func ms(v float64) time.Duration { return time.Duration(v * float64(time.Millisecond)) }

func probe(req probeRequest) (any, error) {
	if req.PeriodMs <= 0 {
		return nil, fmt.Errorf("period must be positive, got %vms", req.PeriodMs)
	}
	period := ms(req.PeriodMs)
	c := clock.Measure(req.Samples)

	out := probeResult{
		Host:                      run.DescribeHost(),
		Clock:                     c,
		PeriodNs:                  int64(period),
		MinimumResolvablePeriodNs: int64(c.MinimumResolvablePeriod()),
		SleepCanHold:              c.SleepCanHold(period),
	}
	if err := c.CheckAdequate(period); err != nil {
		out.AdequateError = err.Error()
	}
	return out, nil
}

func once(req onceRequest) (any, error) {
	valid := false
	for _, k := range pacer.Kinds() {
		if string(k) == req.Kind {
			valid = true
		}
	}
	if !valid {
		return nil, fmt.Errorf("unknown strategy %q", req.Kind)
	}

	// Only the idle scenario. The contention and churn generators are goroutines
	// that never block, and without asynchronous preemption, which js/wasm does
	// not have, the first one scheduled would never give the thread back and the
	// paced loop would never wake. The page applies CPU contention from Web
	// Workers on other cores instead.
	rep, err := run.Once(run.Config{
		Kind:     pacer.Kind(req.Kind),
		Period:   ms(req.PeriodMs),
		Work:     ms(req.WorkMs),
		Ticks:    req.Ticks,
		Warmup:   req.Warmup,
		Scenario: run.Scenario{Name: "idle"},
		Opts:     pacer.Options{SpinSlack: ms(req.SpinSlackMs)},
	})
	if err != nil {
		return nil, err
	}

	lateness, interval := rep.Samples()
	return onceResult{
		Kind:            req.Kind,
		Lateness:        rep.Lateness,
		Interval:        rep.Interval,
		MissedDeadlines: rep.MissedDeadlines,
		MissRate:        rep.MissRate,
		DriftPerTick:    rep.DriftPerTick,
		FinalLateness:   rep.FinalLateness,
		ElapsedNs:       int64(rep.Elapsed),
		Samples:         lateness,
		Intervals:       interval,
	}, nil
}

// async wraps fn as a JavaScript function taking one JSON string and returning
// a Promise of a JSON string. Errors reject with an Error rather than resolving
// with a sentinel, so the page cannot mistake a refusal for a result.
func async[T any](fn func(T) (any, error)) js.Func {
	return js.FuncOf(func(_ js.Value, args []js.Value) any {
		var raw string
		if len(args) > 0 {
			raw = args[0].String()
		}

		executor := js.FuncOf(func(_ js.Value, p []js.Value) any {
			resolve, reject := p[0], p[1]
			go func() {
				fail := func(err error) {
					reject.Invoke(js.Global().Get("Error").New(err.Error()))
				}

				var req T
				if err := json.Unmarshal([]byte(raw), &req); err != nil {
					fail(fmt.Errorf("bad request: %w", err))
					return
				}
				v, err := fn(req)
				if err != nil {
					fail(err)
					return
				}
				b, err := json.Marshal(v)
				if err != nil {
					fail(errors.New("encoding result: " + err.Error()))
					return
				}
				resolve.Invoke(string(b))
			}()
			return nil
		})
		return js.Global().Get("Promise").New(executor)
	})
}

func main() {
	js.Global().Set("cadence", js.ValueOf(map[string]any{
		"probe":     async(probe),
		"once":      async(once),
		"summarize": async(summarize),
	}))

	// Block forever so the exports stay callable.
	select {}
}

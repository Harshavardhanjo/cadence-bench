// Mirrors the JSON produced by wasm/main.go. Durations are nanoseconds, as in
// the command line tool's own JSON report, and converted only for display.

export type Kind = "sleep-delta" | "absolute-deadline" | "ticker" | "spin-tail";

export const KINDS: Kind[] = ["sleep-delta", "absolute-deadline", "ticker", "spin-tail"];

export type Summary = {
  count: number;
  min: number;
  max: number;
  mean: number;
  stddev: number;
  p50: number;
  p90: number;
  p99: number;
  p999: number;
};

export type Host = {
  goos: string;
  goarch: string;
  go_version: string;
  num_cpu: number;
  gomaxprocs: number;
};

export type Probe = {
  host: Host;
  clock: {
    resolution_ns: Summary;
    sleep_granularity_ns: Summary;
    clock_read_cost_ns: Summary;
    requested_sleep_ns: number;
  };
  periodNs: number;
  minimumResolvablePeriodNs: number;
  adequateError?: string;
  sleepCanHold: boolean;
};

export type OnceRequest = {
  kind: Kind;
  periodMs: number;
  workMs: number;
  ticks: number;
  warmup: number;
  spinSlackMs: number;
};

export type Rep = {
  kind: Kind;
  latenessNs: Summary;
  intervalNs: Summary;
  missedDeadlines: number;
  missRate: number;
  driftPerTickNs: number;
  finalLatenessNs: number;
  elapsedNs: number;
  latenessSeriesNs: number[];
  intervalSeriesNs: number[];
};

/** Median across repetitions with the observed range, as the CLI reports it. */
export type Spread = { median: number; min: number; max: number };

export type Series = {
  kind: Kind;
  reps: Rep[];
  medianLateness: Spread;
  p99Lateness: Spread;
  maxLateness: Spread;
  driftPerTick: Spread;
  missRate: Spread;
};

export type RunSettings = {
  periodMs: number;
  workMs: number;
  ticks: number;
  warmup: number;
  reps: number;
  spinSlackMs: number;
  burners: number;
};

export type Measurement = {
  settings: RunSettings;
  series: Series[];
  forced: boolean;
  probe: Probe;
  isolated: boolean;
};

/** Formats nanoseconds at the scale the CLI uses: microseconds below a millisecond. */
export function durNs(ns: number): string {
  const a = Math.abs(ns);
  if (a >= 1e9) return `${(ns / 1e9).toFixed(2)}s`;
  if (a >= 1e6) return `${(ns / 1e6).toFixed(2)}ms`;
  if (a >= 1e3) return `${(ns / 1e3).toFixed(1)}us`;
  return `${ns.toFixed(0)}ns`;
}

export const KIND_BLURB: Record<Kind, string> = {
  "sleep-delta": "sleeps a full period after each tick's work",
  "absolute-deadline": "sleeps until origin + n × period",
  ticker: "receives from time.Ticker",
  "spin-tail": "sleeps to within a slack window, then busy-waits",
};

// One hue per strategy, legible on both themes. sleep-delta gets the warm one
// because it is the baseline the others are judged against.
export const KIND_COLOR: Record<Kind, string> = {
  "sleep-delta": "#e0572b",
  "absolute-deadline": "#2f7ed8",
  ticker: "#8a63d2",
  "spin-tail": "#1f9d74",
};

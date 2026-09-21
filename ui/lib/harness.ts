// Drives the Go harness in its Web Worker, and the burn workers that load it.
//
// Nothing here computes a figure. Every lateness, drift and percentile comes
// from the Go code the command line tool runs, including the aggregation across
// repetitions, so the page can only present what the harness measured.

import type { Kind, Measurement, OnceRequest, Probe, Rep, RunSettings, Series, Spread, Summary } from "./types";
import { KINDS } from "./types";

type Pending = { resolve: (v: unknown) => void; reject: (e: Error) => void };

let worker: Worker | null = null;
let nextId = 0;
const pending = new Map<number, Pending>();

function getWorker(): Worker {
  if (worker) return worker;
  const w = new Worker("./cadence-worker.js");
  w.onmessage = (e: MessageEvent<{ id: number; result?: unknown; error?: string }>) => {
    const p = pending.get(e.data.id);
    if (!p) return;
    pending.delete(e.data.id);
    if (e.data.error !== undefined) p.reject(new Error(e.data.error));
    else p.resolve(e.data.result);
  };
  w.onerror = (e) => {
    const err = new Error(e.message || "the measurement worker failed to start");
    for (const p of pending.values()) p.reject(err);
    pending.clear();
    worker = null;
  };
  worker = w;
  return w;
}

function call<T>(fn: string, arg: unknown): Promise<T> {
  const w = getWorker();
  const id = nextId++;
  return new Promise<T>((resolve, reject) => {
    pending.set(id, { resolve: resolve as (v: unknown) => void, reject });
    w.postMessage({ id, fn, arg });
  });
}

// A few dozen samples rather than the CLI's 200. The clock read cost figure
// times 200,000 reads per sample, and in a browser every read crosses from Go
// into JavaScript, so the CLI's default would hold the page on "probing" for
// tens of seconds without changing any conclusion drawn from it.
const PROBE_SAMPLES = 40;

export function probe(periodMs: number): Promise<Probe> {
  return call<Probe>("probe", { samples: PROBE_SAMPLES, periodMs });
}

async function spread(values: number[]): Promise<Spread> {
  const s = await call<Summary>("summarize", { values });
  return { median: s.p50, min: s.min, max: s.max };
}

function startBurners(n: number): () => void {
  const ws: Worker[] = [];
  for (let i = 0; i < n; i++) ws.push(new Worker("./burn-worker.js"));
  return () => ws.forEach((w) => w.terminate());
}

export type Progress = { done: number; total: number; kind: Kind; rep: number };

export async function measure(
  settings: RunSettings,
  probeResult: Probe,
  forced: boolean,
  onProgress: (p: Progress) => void,
): Promise<Measurement> {
  const byKind = new Map<Kind, Rep[]>(KINDS.map((k) => [k, []]));
  const total = settings.reps * KINDS.length;
  let done = 0;

  const stop = startBurners(settings.burners);
  try {
    // Give the burn workers time to start, so the first measured ticks are not
    // the only quiet ones. The CLI waits 50ms for goroutines; a new Worker has a
    // script to fetch and compile first.
    if (settings.burners > 0) await new Promise((r) => setTimeout(r, 300));

    // Strategies are interleaved within each repetition rather than run back to
    // back. Background conditions on a visitor's machine change over seconds,
    // and interleaving spreads that change across all four instead of landing
    // it on whichever strategy happened to run last.
    for (let rep = 0; rep < settings.reps; rep++) {
      for (const kind of KINDS) {
        onProgress({ done, total, kind, rep: rep + 1 });
        const req: OnceRequest = {
          kind,
          periodMs: settings.periodMs,
          workMs: settings.workMs,
          ticks: settings.ticks,
          warmup: settings.warmup,
          spinSlackMs: settings.spinSlackMs,
        };
        byKind.get(kind)!.push(await call<Rep>("once", req));
        done++;
      }
    }
  } finally {
    stop();
  }
  onProgress({ done, total, kind: KINDS[KINDS.length - 1], rep: settings.reps });

  const series: Series[] = [];
  for (const kind of KINDS) {
    const reps = byKind.get(kind)!;
    series.push({
      kind,
      reps,
      medianLateness: await spread(reps.map((r) => r.latenessNs.p50)),
      p99Lateness: await spread(reps.map((r) => r.latenessNs.p99)),
      maxLateness: await spread(reps.map((r) => r.latenessNs.max)),
      driftPerTick: await spread(reps.map((r) => r.driftPerTickNs)),
      missRate: await spread(reps.map((r) => r.missRate)),
    });
  }

  return {
    settings,
    series,
    forced,
    probe: probeResult,
    isolated: typeof crossOriginIsolated !== "undefined" && crossOriginIsolated,
  };
}

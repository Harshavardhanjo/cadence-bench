"use client";

import { useCallback, useEffect, useRef, useState } from "react";

import { Controls } from "@/components/Controls";
import { LatenessChart } from "@/components/LatenessChart";
import { ProbePanel } from "@/components/ProbePanel";
import { ResultsTable } from "@/components/ResultsTable";
import { Verdict } from "@/components/Verdict";
import { measure, probe as runProbe } from "@/lib/harness";
import type { Measurement, Probe, RunSettings } from "@/lib/types";

const DEFAULTS: RunSettings = {
  periodMs: 20,
  workMs: 2,
  ticks: 150,
  warmup: 30,
  reps: 1,
  spinSlackMs: 6,
  burners: 0,
};

const link = "text-sky-700 underline underline-offset-2 dark:text-sky-400";

export default function Page() {
  const [settings, setSettings] = useState<RunSettings>(DEFAULTS);
  const [probe, setProbe] = useState<Probe | null>(null);
  const [probing, setProbing] = useState(false);
  const [force, setForce] = useState(false);
  const [running, setRunning] = useState(false);
  const [progress, setProgress] = useState("");
  const [result, setResult] = useState<Measurement | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [cores, setCores] = useState(1);
  const [isolated, setIsolated] = useState(false);

  // Read after mount: neither exists during the static export's prerender.
  useEffect(() => {
    setCores(navigator.hardwareConcurrency || 1);
    setIsolated(typeof crossOriginIsolated !== "undefined" && crossOriginIsolated);
  }, []);

  // The gate depends on the period, so a new period means a new probe. Stale
  // responses are dropped rather than raced: only the latest request may land.
  const probeSeq = useRef(0);
  useEffect(() => {
    const seq = ++probeSeq.current;
    setProbing(true);
    setForce(false);
    runProbe(settings.periodMs)
      .then((p) => {
        if (seq === probeSeq.current) setProbe(p);
      })
      .catch((e: unknown) => {
        if (seq === probeSeq.current) setError(e instanceof Error ? e.message : String(e));
      })
      .finally(() => {
        if (seq === probeSeq.current) setProbing(false);
      });
  }, [settings.periodMs]);

  const gated = !!probe?.adequateError;
  const canRun = !!probe && !probing && (!gated || force);

  const onRun = useCallback(async () => {
    if (!probe) return;
    setRunning(true);
    setError(null);
    try {
      const m = await measure(settings, probe, gated && force, (p) =>
        setProgress(`Measuring ${p.kind}${settings.reps > 1 ? `, repetition ${p.rep}` : ""} (${p.done}/${p.total})…`),
      );
      setResult(m);
    } catch (e: unknown) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setRunning(false);
      setProgress("");
    }
  }, [settings, probe, gated, force]);

  return (
    <main className="mx-auto max-w-6xl px-4 py-10 sm:px-6">
      <header className="max-w-3xl">
        <h1 className="text-2xl font-semibold tracking-tight">cadence-bench</h1>
        <p className="mt-3 text-[15px] leading-relaxed text-neutral-700 dark:text-neutral-300">
          A real-time audio path hands the transport a 20ms frame every 20ms. The loop almost everyone writes first
          sleeps a period after each frame&apos;s work, and it cannot hold that rate: every tick it falls behind by
          however long the work took, and it never catches up. It sounds like occasional choppiness under load, and it
          does not reproduce on an idle laptop.
        </p>
        <div className="mt-4 rounded-lg border border-sky-200 bg-sky-50 p-3 text-[13px] leading-relaxed text-sky-950 dark:border-sky-900 dark:bg-sky-950/40 dark:text-sky-100">
          <p className="font-semibold">This page measures your browser.</p>
          <p className="mt-1">
            Unlike its sibling benchmarks, cadence-bench is a measurement, not a simulation, so the machine running it
            is the subject. Here that is the Go harness compiled to WebAssembly, in a Web Worker, on a single thread,
            sleeping on setTimeout and reading performance.now. The absolute figures describe this browser. For Go on
            real servers, see the{" "}
            <a href="https://github.com/Harshavardhanjo/cadence-bench#results" className={link}>
              published results from Linux, macOS and Windows
            </a>
            . The ordering of the strategies is what carries over.
          </p>
        </div>
      </header>

      {error && (
        <div className="mt-8 rounded-lg border border-rose-300 bg-rose-50 p-4 text-[13px] text-rose-900 dark:border-rose-900 dark:bg-rose-950/40 dark:text-rose-200">
          <p className="font-semibold">The harness reported an error.</p>
          <p className="mt-1 font-mono text-[12px]">{error}</p>
        </div>
      )}

      <div className="mt-8">
        <ProbePanel probe={probe} probing={probing} isolated={isolated} cores={cores} force={force} onForceChange={setForce} />
      </div>

      <div className="mt-6 grid gap-6 lg:grid-cols-[320px_minmax(0,1fr)]">
        <aside>
          <Controls
            settings={settings}
            cores={cores}
            canRun={canRun}
            running={running}
            progress={progress}
            onChange={(patch) => setSettings((s) => ({ ...s, ...patch }))}
            onRun={() => void onRun()}
          />
        </aside>

        <section className="space-y-5">
          {result ? (
            <>
              <Verdict m={result} />
              <LatenessChart m={result} />
              <ResultsTable m={result} />
            </>
          ) : (
            <div className="flex min-h-[240px] items-center justify-center rounded-lg border border-dashed border-neutral-300 p-6 text-center text-[13px] text-neutral-500 dark:border-neutral-700">
              {running
                ? progress
                : gated && !force
                  ? "The clock gate has refused this period. Choose a longer one, or override the gate above."
                  : "Run a measurement to see each strategy's lateness, tick by tick."}
            </div>
          )}
        </section>
      </div>

      <footer className="mt-12 space-y-3 border-t border-neutral-200 pt-6 text-[12px] leading-relaxed text-neutral-500 dark:border-neutral-800">
        <p>
          Built by{" "}
          <a href="https://harshavardhanjo.com" className={link}>
            Harshavardhan Jothikumar
          </a>
          . Source, methodology and full results:{" "}
          <a href="https://github.com/Harshavardhanjo/cadence-bench" className={link}>
            github.com/Harshavardhanjo/cadence-bench
          </a>
          .
        </p>
        <p>
          What differs from the command line tool, and why. The cpu-contention and gc-churn scenarios start goroutines
          that never block. Go&apos;s WebAssembly runtime has one thread and no asynchronous preemption, so such a
          goroutine would take the thread and the paced loop would never wake. Contention comes from Web Workers
          burning other cores instead, and GC churn is not offered. The probe takes 40 samples rather than 200, because
          every clock read crosses from Go into JavaScript. Everything else, from the four strategies and the lateness
          and drift definitions to the percentiles and the clock gate, is the same Go code.
        </p>
        <p>
          One of three measuring the same audio path. This one asks whether the send loop can hold its deadline;{" "}
          <a href="https://jitter-bench.harshavardhanjo.com" className={link}>
            jitter-bench
          </a>{" "}
          asks what the receive buffer costs to keep audio continuous; and{" "}
          <a href="https://turn-bench.harshavardhanjo.com" className={link}>
            turn-bench
          </a>{" "}
          asks what an agent should record when it is interrupted. They share a statistics package so their numbers
          are comparable.
        </p>
      </footer>
    </main>
  );
}

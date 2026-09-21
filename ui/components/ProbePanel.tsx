"use client";

import type { Probe } from "@/lib/types";
import { durNs } from "@/lib/types";

type Props = {
  probe: Probe | null;
  probing: boolean;
  isolated: boolean;
  cores: number;
  force: boolean;
  onForceChange: (v: boolean) => void;
};

function Figure({ label, value, hint }: { label: string; value: string; hint: string }) {
  return (
    <div>
      <p className="text-[11px] uppercase tracking-wide text-neutral-500">{label}</p>
      <p className="mt-0.5 font-mono text-lg tabular-nums">{value}</p>
      <p className="mt-0.5 text-[11px] leading-snug text-neutral-500">{hint}</p>
    </div>
  );
}

export function ProbePanel({ probe, probing, isolated, cores, force, onForceChange }: Props) {
  return (
    <section className="rounded-lg border border-neutral-200 bg-white p-4 dark:border-neutral-800 dark:bg-neutral-950">
      <div className="flex flex-wrap items-baseline justify-between gap-2">
        <h2 className="text-sm font-semibold">1. What this browser&apos;s clock can support</h2>
        <span className="text-[12px] text-neutral-500">
          {cores} core{cores === 1 ? "" : "s"} ·{" "}
          {isolated ? "cross-origin isolated (finer clock)" : "not cross-origin isolated (coarsened clock)"}
        </span>
      </div>

      {!probe && <p className="mt-3 text-[13px] text-neutral-500">{probing ? "Probing the clock and timers…" : ""}</p>}

      {probe && (
        <>
          <div className={`mt-4 grid gap-4 sm:grid-cols-3 ${probing ? "opacity-50" : ""}`}>
            <Figure
              label="clock resolution"
              value={durNs(probe.clock.resolution_ns.p50)}
              hint="The smallest step performance.now takes. Browsers coarsen it on purpose, as a Spectre mitigation."
            />
            <Figure
              label="sleep granularity"
              value={durNs(probe.clock.sleep_granularity_ns.p50)}
              hint="What a 1ms sleep actually takes. In a browser, time.Sleep is setTimeout."
            />
            <Figure
              label="clock read cost"
              value={durNs(probe.clock.clock_read_cost_ns.p50)}
              hint="One time.Now, which here crosses from Go into JavaScript. Spin-tail pays it every iteration."
            />
          </div>

          {probe.adequateError ? (
            <div className="mt-4 rounded border border-amber-300 bg-amber-50 p-3 text-[13px] dark:border-amber-900 dark:bg-amber-950/40">
              <p className="font-semibold text-amber-900 dark:text-amber-200">
                This browser cannot measure a {probe.periodNs / 1e6}ms period, so the harness refuses to.
              </p>
              <p className="mt-1 font-mono text-[12px] leading-relaxed text-amber-900 dark:text-amber-200">
                {probe.adequateError}
              </p>
              <p className="mt-2 text-[12px] leading-relaxed text-amber-900 dark:text-amber-200">
                This is the command line tool&apos;s own clock gate, not a check written for this page. Choose a period of
                at least {durNs(probe.minimumResolvablePeriodNs)}, or override it the way <span className="font-mono">-force</span>{" "}
                does and read only the drift.
              </p>
              <label className="mt-2 flex items-center gap-2 text-[12px] text-amber-900 dark:text-amber-200">
                <input type="checkbox" checked={force} onChange={(e) => onForceChange(e.target.checked)} className="accent-amber-600" />
                measure anyway
              </label>
            </div>
          ) : (
            <p className="mt-4 text-[13px] text-emerald-800 dark:text-emerald-300">
              The clock can resolve a {probe.periodNs / 1e6}ms period: every lateness figure is quantised to{" "}
              {durNs(probe.clock.resolution_ns.p50)}, and the harness requires 100× finer than the period.
            </p>
          )}

          {!probe.sleepCanHold && (
            <p className="mt-2 text-[13px] text-amber-800 dark:text-amber-300">
              Sleeping cannot hold this period here: the shortest sleep exceeds it, so only spin-tail can keep up.
            </p>
          )}
        </>
      )}
    </section>
  );
}

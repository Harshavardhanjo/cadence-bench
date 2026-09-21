"use client";

import type { Measurement, Spread } from "@/lib/types";
import { durNs, KIND_BLURB, KIND_COLOR } from "@/lib/types";

function range(s: Spread): string {
  return `${durNs(s.min)}..${durNs(s.max)}`;
}

// The same columns as the command line tool's table, in the same units, so a
// row here and a row in the README can be read side by side.
export function ResultsTable({ m }: { m: Measurement }) {
  const multi = m.settings.reps > 1;
  return (
    <div className="overflow-x-auto rounded-lg border border-neutral-200 bg-white dark:border-neutral-800 dark:bg-neutral-950">
      <table className="w-full min-w-[640px] text-left text-[12px]">
        <thead className="border-b border-neutral-200 text-neutral-500 dark:border-neutral-800">
          <tr>
            <th className="px-3 py-2 font-medium">strategy</th>
            <th className="px-3 py-2 text-right font-medium">median</th>
            <th className="px-3 py-2 text-right font-medium">p99{multi && " (range)"}</th>
            <th className="px-3 py-2 text-right font-medium">worst</th>
            <th className="px-3 py-2 text-right font-medium">drift/tick</th>
            <th className="px-3 py-2 text-right font-medium">missed</th>
          </tr>
        </thead>
        <tbody className="font-mono tabular-nums">
          {m.series.map((s) => (
            <tr key={s.kind} className="border-b border-neutral-100 last:border-0 dark:border-neutral-900">
              <td className="px-3 py-2 font-sans">
                <span className="flex items-center gap-2">
                  <span className="inline-block h-2 w-2 rounded-full" style={{ background: KIND_COLOR[s.kind] }} />
                  <span className="font-mono">{s.kind}</span>
                </span>
                <span className="mt-0.5 block pl-4 text-[11px] text-neutral-500">{KIND_BLURB[s.kind]}</span>
              </td>
              <td className="px-3 py-2 text-right">{durNs(s.medianLateness.median)}</td>
              <td className="px-3 py-2 text-right">
                {durNs(s.p99Lateness.median)}
                {multi && <span className="block text-[11px] text-neutral-500">{range(s.p99Lateness)}</span>}
              </td>
              <td className="px-3 py-2 text-right">{durNs(s.maxLateness.median)}</td>
              <td className="px-3 py-2 text-right">{durNs(s.driftPerTick.median)}</td>
              <td className="px-3 py-2 text-right">{(s.missRate.median * 100).toFixed(1)}%</td>
            </tr>
          ))}
        </tbody>
      </table>
      <p className="border-t border-neutral-200 px-3 py-2 text-[11px] leading-snug text-neutral-500 dark:border-neutral-800">
        Lateness past each tick&apos;s deadline. Each figure is the median across repetitions of that repetition&apos;s own
        statistic, computed by the Go stats package the sibling benchmarks share. Missed means woken more than one full
        period late, so the next frame was already due.
      </p>
    </div>
  );
}

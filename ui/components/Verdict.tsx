"use client";

import type { Measurement } from "@/lib/types";
import { verdict } from "@/lib/analysis";

const TONE = {
  drift: "border-sky-300 bg-sky-50 dark:border-sky-900 dark:bg-sky-950/40",
  level: "border-neutral-300 bg-neutral-50 dark:border-neutral-800 dark:bg-neutral-900/60",
  forced: "border-amber-300 bg-amber-50 dark:border-amber-900 dark:bg-amber-950/40",
};

export function Verdict({ m }: { m: Measurement }) {
  const v = verdict(m);
  return (
    <div className={`rounded-lg border p-4 ${TONE[v.tone]}`}>
      <p className="text-sm font-semibold text-neutral-900 dark:text-neutral-100">{v.headline}</p>
      {v.detail.map((d, i) => (
        <p key={i} className="mt-1.5 text-[13px] leading-relaxed text-neutral-700 dark:text-neutral-300">
          {d}
        </p>
      ))}
    </div>
  );
}

"use client";

import { useMemo, useState } from "react";
import type { Kind, Measurement } from "@/lib/types";
import { KIND_COLOR } from "@/lib/types";

// Lateness per tick, one line per strategy, from a single repetition's raw
// series. This is the picture the tables cannot give: a loop that holds its
// rate is a flat band however noisy, and a drifting loop is a ramp. The CLI
// summarises the ramp as drift per tick; here it is visible.

const W = 760;
const H = 360;
const PAD = { top: 20, right: 132, bottom: 44, left: 64 };

function niceStep(raw: number): number {
  if (raw <= 0) return 1;
  const mag = Math.pow(10, Math.floor(Math.log10(raw)));
  const norm = raw / mag;
  return (norm <= 1 ? 1 : norm <= 2 ? 2 : norm <= 5 ? 5 : 10) * mag;
}

function ticks(min: number, max: number, target: number): number[] {
  const step = niceStep((max - min) / target);
  const out: number[] = [];
  for (let v = Math.ceil(min / step) * step; v <= max + step / 2; v += step) out.push(Number(v.toFixed(6)));
  return out;
}

export function LatenessChart({ m }: { m: Measurement }) {
  const [includeNaive, setIncludeNaive] = useState(true);
  const [rep, setRep] = useState(0);
  const periodMs = m.settings.periodMs;
  const repCount = m.series[0]?.reps.length ?? 0;
  const repIndex = Math.min(rep, Math.max(0, repCount - 1));

  const model = useMemo(() => {
    const lines = m.series
      .filter((s) => includeNaive || s.kind !== "sleep-delta")
      .map((s) => ({ kind: s.kind, ms: (s.reps[repIndex]?.latenessSeriesNs ?? []).map((v) => v / 1e6) }))
      .filter((l) => l.ms.length > 0);
    if (lines.length === 0) return null;

    const n = Math.max(...lines.map((l) => l.ms.length));
    const all = lines.flatMap((l) => l.ms);
    const yMin = Math.min(0, ...all);
    const yMaxRaw = Math.max(...all);
    const yMax = yMaxRaw <= yMin ? yMin + 1 : yMaxRaw + (yMaxRaw - yMin) * 0.06;

    const plotW = W - PAD.left - PAD.right;
    const plotH = H - PAD.top - PAD.bottom;
    const sx = (i: number) => PAD.left + (n <= 1 ? 0 : (i / (n - 1)) * plotW);
    const sy = (v: number) => PAD.top + plotH - ((v - yMin) / (yMax - yMin)) * plotH;

    const paths = lines.map((l) => ({
      kind: l.kind,
      d: l.ms.map((v, i) => `${i === 0 ? "M" : "L"}${sx(i).toFixed(1)},${sy(v).toFixed(1)}`).join(""),
      endY: sy(l.ms[l.ms.length - 1]),
      endMs: l.ms[l.ms.length - 1],
    }));

    // Direct labels at each line's end, nudged apart so they cannot overlap.
    const labels = [...paths].sort((a, b) => a.endY - b.endY);
    for (let i = 1; i < labels.length; i++) {
      if (labels[i].endY - labels[i - 1].endY < 14) labels[i].endY = labels[i - 1].endY + 14;
    }

    return {
      paths,
      labels,
      n,
      xTicks: ticks(0, n - 1, 6),
      yTicks: ticks(yMin, yMax, 5),
      sx,
      sy,
      yMin,
      yMax,
      plotW,
      plotH,
    };
  }, [m, includeNaive, repIndex]);

  if (!model) return null;
  const missedVisible = periodMs >= model.yMin && periodMs <= model.yMax;

  return (
    <figure className="rounded-lg border border-neutral-200 bg-white p-4 dark:border-neutral-800 dark:bg-neutral-950">
      <figcaption className="flex flex-wrap items-baseline justify-between gap-x-4 gap-y-2">
        <span className="text-sm font-semibold">Lateness past each tick&apos;s deadline</span>
        <span className="flex flex-wrap items-center gap-3 text-[12px] text-neutral-600 dark:text-neutral-400">
          <label className="flex items-center gap-1.5">
            <input
              type="checkbox"
              checked={includeNaive}
              onChange={(e) => setIncludeNaive(e.target.checked)}
              className="accent-sky-600"
            />
            include sleep-delta
          </label>
          {repCount > 1 && (
            <label className="flex items-center gap-1.5">
              repetition
              <select
                value={repIndex}
                onChange={(e) => setRep(Number(e.target.value))}
                className="rounded border border-neutral-300 bg-white px-1 py-0.5 dark:border-neutral-700 dark:bg-neutral-900"
              >
                {Array.from({ length: repCount }, (_, i) => (
                  <option key={i} value={i}>
                    {i + 1}
                  </option>
                ))}
              </select>
            </label>
          )}
        </span>
      </figcaption>

      <svg viewBox={`0 0 ${W} ${H}`} className="mt-3 w-full" role="img" aria-label="Lateness per tick for each pacing strategy">
        {model.yTicks.map((v) => (
          <g key={`y${v}`}>
            <line
              x1={PAD.left}
              x2={PAD.left + model.plotW}
              y1={model.sy(v)}
              y2={model.sy(v)}
              className="stroke-neutral-200 dark:stroke-neutral-800"
            />
            <text x={PAD.left - 8} y={model.sy(v) + 4} textAnchor="end" className="fill-neutral-500 text-[11px] tabular-nums">
              {v}ms
            </text>
          </g>
        ))}
        {model.xTicks.map((v) => (
          <text
            key={`x${v}`}
            x={model.sx(v)}
            y={H - PAD.bottom + 18}
            textAnchor="middle"
            className="fill-neutral-500 text-[11px] tabular-nums"
          >
            {v + 1}
          </text>
        ))}
        <text x={PAD.left + model.plotW / 2} y={H - 6} textAnchor="middle" className="fill-neutral-500 text-[11px]">
          measured tick
        </text>

        {missedVisible && (
          <g>
            <line
              x1={PAD.left}
              x2={PAD.left + model.plotW}
              y1={model.sy(periodMs)}
              y2={model.sy(periodMs)}
              strokeDasharray="4 4"
              className="stroke-rose-500"
            />
            <text x={PAD.left + 4} y={model.sy(periodMs) - 5} className="fill-rose-600 text-[11px] dark:fill-rose-400">
              one period late: the next frame is already due
            </text>
          </g>
        )}

        {model.paths.map((p) => (
          <path key={p.kind} d={p.d} fill="none" stroke={KIND_COLOR[p.kind as Kind]} strokeWidth={1.6} strokeLinejoin="round" />
        ))}
        {model.labels.map((p) => (
          <text
            key={`l${p.kind}`}
            x={PAD.left + model.plotW + 8}
            y={p.endY + 4}
            fill={KIND_COLOR[p.kind as Kind]}
            className="text-[11px] font-medium"
          >
            {p.kind}
          </text>
        ))}
      </svg>
      <p className="mt-2 text-[11px] leading-snug text-neutral-500">
        Repetition {repIndex + 1} of {repCount}, raw per-tick values after {m.settings.warmup} warmup ticks. A flat band
        holds its rate however noisy it is; a ramp is drift, and it does not stop.
      </p>
    </figure>
  );
}

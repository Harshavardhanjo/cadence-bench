"use client";

import type { RunSettings } from "@/lib/types";

type Props = {
  settings: RunSettings;
  cores: number;
  canRun: boolean;
  running: boolean;
  progress: string;
  onChange: (patch: Partial<RunSettings>) => void;
  onRun: () => void;
};

type SliderProps = {
  label: string;
  hint?: string;
  value: number;
  min: number;
  max: number;
  step: number;
  format: (v: number) => string;
  onChange: (v: number) => void;
  disabled?: boolean;
};

function Slider({ label, hint, value, min, max, step, format, onChange, disabled }: SliderProps) {
  return (
    <label className="block">
      <span className="flex items-baseline justify-between gap-2">
        <span className="text-[13px] font-medium text-neutral-800 dark:text-neutral-200">{label}</span>
        <span className="font-mono text-[12px] tabular-nums text-neutral-600 dark:text-neutral-400">{format(value)}</span>
      </span>
      <input
        type="range"
        min={min}
        max={max}
        step={step}
        value={value}
        disabled={disabled}
        onChange={(e) => onChange(Number(e.target.value))}
        className="mt-1 w-full accent-sky-600"
      />
      {hint && <span className="mt-0.5 block text-[11px] leading-snug text-neutral-500">{hint}</span>}
    </label>
  );
}

const PERIODS = [10, 20, 40, 60];
const msFmt = (v: number) => `${v}ms`;

// Roughly how long a run takes: every strategy sleeps (ticks + warmup) periods
// per repetition, and sleep-delta takes longer again by its own drift. Shown so
// that nobody sits watching a spinner wondering whether it hung.
function estimateSeconds(s: RunSettings): number {
  const perRep = (s.ticks + s.warmup) * s.periodMs * 4 + (s.ticks + s.warmup) * (s.workMs + 2);
  return Math.ceil((perRep * s.reps) / 1000);
}

export function Controls({ settings: s, cores, canRun, running, progress, onChange, onRun }: Props) {
  return (
    <div className="space-y-5 rounded-lg border border-neutral-200 bg-white p-4 dark:border-neutral-800 dark:bg-neutral-950">
      <h2 className="text-sm font-semibold">2. Measure the four strategies</h2>

      <div>
        <span className="text-[13px] font-medium text-neutral-800 dark:text-neutral-200">Period</span>
        <div className="mt-1 grid grid-cols-4 gap-1">
          {PERIODS.map((p) => (
            <button
              key={p}
              onClick={() => onChange({ periodMs: p, spinSlackMs: Math.min(s.spinSlackMs, p / 2) })}
              disabled={running}
              className={`rounded border px-2 py-1 font-mono text-[12px] ${
                p === s.periodMs
                  ? "border-neutral-900 bg-neutral-900 text-white dark:border-neutral-100 dark:bg-neutral-100 dark:text-neutral-900"
                  : "border-neutral-300 dark:border-neutral-700"
              }`}
            >
              {p}ms
            </button>
          ))}
        </div>
        <p className="mt-1 text-[11px] leading-snug text-neutral-500">20ms is one Opus frame at the usual framing.</p>
      </div>

      <div className="space-y-4">
        <Slider
          label="Work per tick"
          hint="Simulated encode work, spun rather than slept. sleep-delta falls this far behind every tick, plus its sleep's overshoot."
          value={s.workMs}
          min={0}
          max={10}
          step={0.5}
          format={(v) => `${v}ms`}
          disabled={running}
          onChange={(v) => onChange({ workMs: v })}
        />
        <Slider
          label="Busy workers"
          hint="Threads burning other cores, the browser's version of the cpu-contention scenario."
          value={s.burners}
          min={0}
          max={Math.max(1, cores)}
          step={1}
          format={(v) => v.toFixed(0)}
          disabled={running}
          onChange={(v) => onChange({ burners: v })}
        />
        <Slider
          label="Spin-tail slack"
          hint="How early spin-tail stops sleeping and starts busy-waiting. Must exceed the sleep granularity or the spin never runs."
          value={s.spinSlackMs}
          min={0.5}
          max={Math.max(0.5, s.periodMs / 2)}
          step={0.5}
          format={msFmt}
          disabled={running}
          onChange={(v) => onChange({ spinSlackMs: v })}
        />
      </div>

      <div className="grid grid-cols-2 gap-3 border-t border-neutral-200 pt-4 dark:border-neutral-800">
        <Slider
          label="Ticks"
          hint={`After ${s.warmup} warmup ticks.`}
          value={s.ticks}
          min={100}
          max={500}
          step={50}
          format={(v) => v.toFixed(0)}
          disabled={running}
          onChange={(v) => onChange({ ticks: v })}
        />
        <Slider
          label="Repetitions"
          hint="Their spread is the error bar."
          value={s.reps}
          min={1}
          max={5}
          step={1}
          format={(v) => v.toFixed(0)}
          disabled={running}
          onChange={(v) => onChange({ reps: v })}
        />
      </div>

      <button
        onClick={onRun}
        disabled={!canRun || running}
        className="w-full rounded bg-neutral-900 px-3 py-2 text-[13px] font-medium text-white transition hover:bg-neutral-700 disabled:opacity-50 dark:bg-neutral-100 dark:text-neutral-900 dark:hover:bg-neutral-300"
      >
        {running ? progress || "Measuring…" : `Measure (about ${estimateSeconds(s)}s)`}
      </button>
      <p className="text-[11px] leading-snug text-neutral-500">
        Keep this tab in front while it runs. A browser is free to slow timers in a tab you are not looking at, and the
        harness would faithfully measure that.
      </p>
    </div>
  );
}

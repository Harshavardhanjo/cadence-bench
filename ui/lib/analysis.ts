// Turns a measurement into the one sentence worth reading first.
//
// This interprets figures; it does not produce them. Every number quoted comes
// from the Go harness unchanged, and the only arithmetic here is extrapolating
// the measured drift per tick to a minute of audio, which is what drift per
// tick means.

import type { Kind, Measurement, Series } from "./types";
import { durNs } from "./types";

export type Tone = "drift" | "level" | "forced";

export type Verdict = { tone: Tone; headline: string; detail: string[] };

const HOLDERS: Kind[] = ["absolute-deadline", "ticker", "spin-tail"];

function find(m: Measurement, kind: Kind): Series | undefined {
  return m.series.find((s) => s.kind === kind);
}

export function verdict(m: Measurement): Verdict {
  const { periodMs, ticks } = m.settings;
  const ticksPerMinute = Math.round(60_000 / periodMs);
  const detail: string[] = [];

  const naive = find(m, "sleep-delta");
  const holders = HOLDERS.map((k) => find(m, k)).filter((s): s is Series => !!s);
  const best = [...holders].sort((a, b) => a.p99Lateness.median - b.p99Lateness.median)[0];

  // "Drifts" means clearly separated from the strategies that cannot drift by
  // construction, not merely non-zero: at a coarse browser clock every figure
  // carries some noise, and a threshold on the absolute value would call noise
  // a finding.
  const holderDrift = Math.max(...holders.map((s) => Math.abs(s.driftPerTick.median)), 0);
  const drift = naive?.driftPerTick.median ?? 0;
  const drifts = !!naive && drift > 10_000 && drift > 10 * holderDrift;

  let tone: Tone = drifts ? "drift" : "level";
  let headline: string;

  if (drifts && naive) {
    const final = naive.reps[0]?.finalLatenessNs ?? 0;
    headline = `sleep-delta fell ${durNs(drift)} further behind on every tick.`;
    detail.push(
      `Over ${ticks} measured ticks it finished ${durNs(final)} late and missed ${(naive.missRate.median * 100).toFixed(0)}% of its deadlines. ` +
        `At that rate a minute of ${periodMs}ms frames (${ticksPerMinute.toLocaleString()} ticks) ends ${durNs(drift * ticksPerMinute)} behind the clock, ` +
        `and a receiver's jitter buffer drains the whole time.`,
    );
    if (best) {
      detail.push(
        `The other three cannot drift by construction, and here they did not: the tightest tail was ${best.kind}, with p99 lateness of ${durNs(best.p99Lateness.median)}.`,
      );
    }
  } else {
    headline = "No strategy separated from the others on this run.";
    detail.push(
      `sleep-delta's drift per tick was ${durNs(drift)}, not clearly apart from the strategies that cannot drift. ` +
        `Its error is the tick's work plus the sleep's overshoot, so raise the work slider and it will show.`,
    );
  }

  if (!m.probe.sleepCanHold) {
    detail.push(
      `This browser's shortest sleep, ${durNs(m.probe.clock.sleep_granularity_ns.p50)}, is longer than the ${periodMs}ms period, so every sleeping strategy is capped by the platform here and only spin-tail can hold the cadence. That is a property of the browser, not a finding about the strategies.`,
    );
  }

  if (m.settings.burners > 0) {
    detail.push(
      `Measured with ${m.settings.burners} busy worker${m.settings.burners === 1 ? "" : "s"} competing for cores.`,
    );
  }

  if (m.forced) {
    tone = "forced";
    detail.push(
      `Measured with the clock gate overridden. This browser's clock cannot resolve the period finely enough to compare strategies, so treat the percentiles as an artefact of the clock and read only the drift.`,
    );
  }

  return { tone, headline, detail };
}

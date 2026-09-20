# cadence-bench

A real-time audio path has to hand a 20ms frame to the transport every 20ms. Miss
the deadline and the receiver's jitter buffer stretches or underruns; drift, and
the sender and receiver clocks separate without bound. `cadence-bench` measures
how well four ways of writing that loop actually hold the deadline — under CPU
contention and garbage collector pressure — and **refuses to report figures its
host's clock cannot support**.

```
go build ./cmd/cadence
./cadence probe          # characterise this host's clock and timers
./cadence run            # compare every strategy across every scenario
```

It exists because the loop almost everyone writes first is structurally
incapable of holding a rate, and the failure is close to invisible until
production: it sounds like occasional choppiness under load, and it does not
reproduce on an idle laptop. The four strategies compared are `sleep-delta`
(sleep a period after each tick's work), `absolute-deadline` (sleep until
`origin + n*period`), `ticker` (`time.Ticker`) and `spin-tail` (sleep to within a
slack window, then busy-wait).

```
cmd/cadence ──┬── internal/clock    clock resolution, sleep granularity, read cost
              └── internal/run ──┬── internal/pacer   the four strategies
                                 ├── internal/load    encode work, CPU contention, GC churn
                                 └── internal/stats   nearest-rank percentiles
```

## What it measures

**Lateness** is the primary metric: how far past its nominal deadline each tick
woke, where tick `n` is due at `origin + n*period`. **Interval** is secondary,
the gap between consecutive wakes. They are kept separate because a loop can be
good at one and bad at the other — a loop whose period is slightly wrong has
near-perfect intervals while drifting away from the wall clock forever, and a
loop that alternates early and late ticks holds its average rate while sounding
broken.

Two derived figures matter more than the percentiles:

- **drift per tick** — lateness accumulated per tick across the run. A loop that
  holds its rate sits near zero however bad its jitter. A drifting loop grows
  without bound, which is a different and worse failure.
- **missed deadlines** — ticks woken more than one full period late, meaning the
  next frame was already due. This is where a frame stops being late and becomes
  lost.

Each configuration runs under four scenarios of increasing hostility: `idle`,
`cpu-contention` (one busy goroutine per core), `gc-churn`, and both together.

## Status of results

**No cross-strategy results table is published yet.** The development host fails
the harness's own adequacy gate, and inventing numbers from it would defeat the
point of building the thing.

That host is Go 1.17.7 on Windows 11, and what it reports about itself is a
finding in its own right:

| property | measured |
|---|---|
| monotonic clock resolution | ~1.0ms (min 0.50ms, max 1.8ms) |
| sleep granularity, 1ms requested | ~15.5ms |
| monotonic clock read cost | ~5ns |

Windows' default timer quantum is 15.6ms and this Go version does not request a
finer one, so `time.Sleep(20ms)` takes about 31ms — two quanta. **A 20ms cadence
cannot be held by sleeping on this configuration at all**, regardless of how the
loop is written. Separately, a monotonic clock that advances in ~1ms steps
quantises every lateness sample to ~1ms, which makes any comparison between
strategies that differ by tens of microseconds meaningless. `cadence run` exits
non-zero rather than print that table; `-force` overrides it and records the
reason in the report's `warnings`, so a forced run cannot be mistaken for a clean
one.

One structural result *is* robust to that quantisation, because its magnitude is
an order of magnitude larger than the clock's step size: `sleep-delta` accumulates
about 11ms of lateness **per tick** at a 20ms period with 2ms of work, and misses
100% of its deadlines within a few hundred ticks, while the other three strategies
hold drift near zero and miss none. That is the difference between a loop that is
merely imprecise and one that is wrong.

Results on hosts that clear the gate will be added with the methodology below
followed exactly, including the Go version, since the sleep and clock behaviour
being measured is a property of the runtime as much as the OS.

## Methodology

Choices that affect whether the numbers mean anything, stated so they can be
argued with:

- **Repetitions, not single runs.** Every figure is the median across repetitions
  of that repetition's own statistic. One unlucky collection moves a p99 by more
  than the gap between the strategies being compared, so a single run measures
  the machine's mood.
- **Error bars are observed min and max across repetitions**, not a standard
  error. Timing tails are not normally distributed; an interval computed as
  though they were would understate them.
- **Percentiles use nearest-rank, not interpolation**, so every published figure
  is a latency some tick actually exhibited rather than a number no tick recorded.
- **Warmup ticks are discarded, not merely run.** The startup transient holds the
  worst samples.
- **Simulated encode work spins; it does not sleep.** Encoding a frame occupies a
  core. Sleeping inside the work would measure the platform's timer granularity a
  second time, inside the thing being timed.
- **Sample buffers are preallocated**, so the measurement loop does not allocate
  and does not put collector work inside the ticks it is timing.
- **No strategy drops ticks of its own accord.** A late tick returns immediately
  and is reported as late. The one exception is `ticker`, where `time.Ticker`
  buffers a single tick and silently discards the rest when the receiver falls
  behind — that is the runtime's behaviour, it cannot be suppressed from outside,
  and it is one of the findings rather than a flaw in the harness.
- **Host and clock characteristics are written into every report.** Results are
  not comparable across machines, and timer behaviour is a property of the OS and
  the runtime version rather than of any strategy.

## What this does not measure

- **Audio quality.** Nothing here encodes, decodes or plays anything. A missed
  deadline is reported as a missed deadline, not as an audible artefact; how bad a
  given miss rate sounds depends on the receiver's jitter buffer and concealment,
  which is a different project.
- **Network behaviour.** No packets are sent. Transport jitter, loss and
  reordering dominate real end-to-end audio latency and none of it appears here.
- **Multi-stream behaviour.** One paced loop is measured at a time. A media server
  carrying hundreds of concurrent calls has scheduler and cache effects this does
  not capture, and the `cpu-contention` scenario is a crude stand-in at best.
- **Python, yet.** A comparison against `asyncio` and threaded loops is the
  obvious next step and is not in this repository.
- **Anything about specific vendors.** No hosted API is called, so nothing here
  measures a provider's latency.
- **Realtime scheduling.** No attempt is made to raise thread priority, pin to a
  core, or request a finer OS timer resolution. Those are the next things to try
  on a host that fails the sleep-granularity check, and measuring their effect is
  future work rather than a claim.

## Running it

```
./cadence probe -samples 200 -period 20ms

./cadence run \
  -period 20ms \
  -work 2ms \
  -ticks 250 \
  -warmup 50 \
  -reps 3 \
  -out results/run.json
```

`-strategies` and `-scenarios` take comma-separated subsets. `-spin-slack` sets
how early `spin-tail` stops sleeping; it has to exceed the host's timer
granularity plus wakeup latency, or the sleep overshoots the deadline and the
busy-wait never runs. A full matrix at the defaults is sixteen configurations and
takes a few minutes.

Results land on stdout as a table and, with `-out`, as JSON carrying every
repetition plus the host and clock data.

## Tests

```
go test ./...
go test -race ./...
```

Timing-dependent tests assert the *shape* of the error rather than wall-clock
figures, so they hold on hosts with very different timer granularity: drift is
unbounded growth proportional to tick count, and a correct pacer's lateness stays
within a constant however long the loop runs. They skip under `-short`.

## License

MIT. See [LICENSE](LICENSE).

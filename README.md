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
cmd/cadence ──┐
ui (wasm) ────┴─┬── internal/clock    clock resolution, sleep granularity, read cost
                └── internal/run ──┬── internal/pacer   the four strategies
                                   ├── internal/load    encode work, CPU contention, GC churn
                                   └── stats            nearest-rank percentiles, shared
```

`stats` is exported rather than internal because
[jitter-bench](https://github.com/Harshavardhanjo/jitter-bench) and
[turn-bench](https://github.com/Harshavardhanjo/turn-bench) import it, so a p99
means the same thing in all three.

## In the browser

There is also a **browser UI** at
**[cadence-bench.harshavardhanjo.com](https://cadence-bench.harshavardhanjo.com)**,
running this harness compiled to WebAssembly. Source in [ui/](ui).

Unlike its siblings' UIs, which run deterministic simulations, this one runs a
measurement, and a measurement describes whatever machine it runs on. The page
therefore measures the visitor's browser: a single-threaded Go runtime in a Web
Worker, sleeping on `setTimeout` and reading a `performance.now` that browsers
deliberately coarsen. It runs the same probe first and the same clock adequacy
gate, and if the gate refuses a period, the page refuses it too, with the
override the CLI calls `-force`. The site is served cross-origin isolated
because that is what earns the finer clock, 5us instead of 100us in Chrome.

Two things differ from the CLI, and the page says so:

- **Contention comes from Web Workers, and GC churn is not offered.** Both CLI
  scenarios start goroutines that never block. `js/wasm` has one thread and no
  asynchronous preemption, so such a goroutine takes the thread and the paced
  loop never wakes again.
- **The probe takes 40 samples instead of 200**, because every clock read
  crosses from Go into JavaScript.

What carries over is the ordering. `sleep-delta` drifts in a browser for the
same reason it drifts on a server.

```
cd ui && npm install && npm run dev
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

## Results

Measured on GitHub-hosted runners, Go 1.27.1, at a 20ms period with 2ms of
simulated encode work: 150 measured ticks after 30 discarded, 3 repetitions. Each
figure is the median across repetitions of that repetition's own statistic, with
the range across repetitions in brackets.

**Read the ordering, not the absolute numbers.** These are shared 3–4 core VMs,
and `cpu-contention` runs one busy goroutine per core on a machine that is
already sharing silicon, so it is a worst case rather than a typical one.
Reproduce on your own hardware before quoting a figure.

### linux/amd64, 4 cores

| strategy | scenario | median | p99 (min..max) | drift/tick | missed |
|---|---|---|---|---|---|
| sleep-delta | idle | 224.38ms | 385.50ms (383.93..385.96) | 2.16ms | 100.0% |
| sleep-delta | cpu-contention | 2156.44ms | 3647.43ms (3619.54..3681.71) | 20.53ms | 100.0% |
| absolute-deadline | idle | 519.6µs | 1.05ms (1.02..1.07) | −889ns | 0.0% |
| absolute-deadline | cpu-contention | 8.78ms | 27.63ms (26.28..33.00) | 45.3µs | 1.3% |
| ticker | idle | 524.4µs | 1.02ms (1.02..1.06) | −689ns | 0.0% |
| ticker | cpu-contention | 21.64ms | 40.53ms (31.12..45.25) | 176.6µs | 56.7% |
| spin-tail | idle | **116ns** | **183ns (149..201)** | 0ns | 0.0% |
| spin-tail | cpu-contention | 8.06ms | 29.17ms (27.06..29.18) | 43.1µs | 2.0% |

### darwin/arm64, 3 cores

| strategy | scenario | median | p99 (min..max) | drift/tick | missed |
|---|---|---|---|---|---|
| sleep-delta | idle | 385.51ms | 689.32ms (684.00..745.00) | 4.06ms | 100.0% |
| sleep-delta | cpu-contention | 5179.33ms | 8419.34ms (8188.64..8588.49) | 46.40ms | 100.0% |
| absolute-deadline | idle | 1.05ms | 9.52ms (7.65..20.48) | 5.7µs | 0.0% |
| absolute-deadline | cpu-contention | 30.79ms | 100.52ms (89.42..105.30) | 298.8µs | 64.0% |
| ticker | idle | 1.10ms | 8.88ms (5.49..17.10) | −15.0µs | 0.0% |
| ticker | cpu-contention | 7207.62ms | 11977.55ms (11422.78..13725.04) | 69.83ms | 100.0% |
| spin-tail | idle | 65.8µs | 63.50ms (37.18..65.17) | −269ns | 6.0% |
| spin-tail | cpu-contention | 55.92ms | 190.52ms (150.35..207.02) | −74.4µs | 79.3% |

The macOS idle rows carry a tail that an idle machine should not produce — a 6%
miss rate for `spin-tail` while doing nothing means the busy-wait goroutine was
descheduled, so that runner was not idle. Those figures describe a noisy shared
VM rather than a property of macOS, and they are reproduced here rather than
quietly dropped.

### What the numbers say

**`sleep-delta` is not slightly worse, it is wrong.** It accumulates 2.16ms of
lateness *per tick* on Linux and misses every deadline within 150 ticks. The
other three miss none on an idle host. This is the difference between a loop
that is imprecise and a loop that cannot hold a rate at all, and it is the loop
most people write first.

**`spin-tail` buys three orders of magnitude, and a core.** 116ns median against
`absolute-deadline`'s 519.6µs on an idle Linux host, with a p99 of 183ns against
1.05ms. Under contention that advantage disappears entirely — 2.0% missed against
1.3% — because a busy-wait cannot help when there is no core to wait on. It is
the right answer for a dedicated audio thread and the wrong one for a loop
sharing a machine.

**`time.Ticker` degrades far worse under load than a hand-written deadline loop.**
On the same Linux host at the same period, `ticker` misses **56.7%** of deadlines
under contention where `absolute-deadline` misses **1.3%**; on macOS it collapses
to 100%. A `Ticker`'s channel buffers a single tick and silently discards the
rest when the receiver falls behind, so the loop is never told. This is the
result worth taking away: the idle columns make `ticker` and `absolute-deadline`
look interchangeable, and they are not.

**A host can hold a cadence it cannot measure.** Windows sleeps accurately enough
for a 20ms period but reads a monotonic clock that advances in ~0.7–1.0ms steps,
so `cadence run` refuses there. The Go upgrade below fixed the first problem and
left the second untouched.

### Platform characteristics

| host | Go | clock resolution | sleep granularity (1ms requested) | read cost |
|---|---|---|---|---|
| linux/amd64, CI | 1.27.1 | 30ns | 1.07ms | 57ns |
| darwin/arm64, CI | 1.27.1 | 42ns | 1.19ms | 66ns |
| windows/amd64, CI | 1.27.1 | 670µs | 1.56ms | 7ns |
| windows/amd64, local | 1.27.0 | 1.00ms | 1.52ms | 5ns |
| windows/amd64, local | 1.17.7 | 1.00ms | **15.52ms** | 5ns |

The last two rows are the same machine before and after a toolchain upgrade.
Under Go 1.17 a 1ms sleep took 15.52ms and `time.Sleep(20ms)` took ~31ms — two
quanta of Windows' 15.6ms default timer — so **a 20ms cadence could not be held
by sleeping at all**, however the loop was written. Under Go 1.27 the same
machine sleeps in ~1.5ms. Clock resolution did not move, which is why both
Windows rows still fail the adequacy gate: a clock quantised to ~1ms cannot
separate strategies that differ by hundreds of nanoseconds.

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

The race detector needs a C toolchain, which on Windows means installing one;
CI runs it on all three platforms regardless.

Timing-dependent tests assert the *shape* of the error rather than wall-clock
figures, so they hold on hosts with very different timer granularity: drift is
unbounded growth proportional to tick count, and a correct pacer's lateness stays
within a constant however long the loop runs. They skip under `-short`.

## License

MIT. See [LICENSE](LICENSE).

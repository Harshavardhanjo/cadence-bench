// Package pacer implements strategies for running a loop at a fixed period.
//
// The motivating case is the send loop of a real-time audio path. An Opus
// frame at the usual 20ms framing must be handed to the transport every 20ms
// of wall clock; if the loop is late the receiver's jitter buffer either
// stretches or underruns, and if the loop drifts the sender and receiver
// clocks separate without bound.
//
// The four strategies here are the ones that appear in real codebases, in
// rough order of how often they are reached for versus how well they hold a
// deadline. Comparing them is the point of this repository: the naive one is
// the most common and the only one that is structurally wrong.
package pacer

import (
	"fmt"
	"time"
)

// Kind identifies a pacing strategy.
type Kind string

const (
	// SleepDelta sleeps for a full period after each tick's work completes.
	//
	// This is the loop almost everyone writes first. It is structurally
	// incapable of holding a rate: the cycle takes period + work + wakeup
	// overhead, so the loop drifts later without bound, proportionally to how
	// long the work takes. It is included as the baseline that the others are
	// measured against.
	SleepDelta Kind = "sleep-delta"

	// AbsoluteDeadline sleeps until a deadline computed from a fixed origin.
	//
	// Tick n is due at origin + n*period regardless of how long previous ticks
	// took, so the error of one tick does not carry into the next and the loop
	// cannot drift. This is the correct default for an audio send loop.
	AbsoluteDeadline Kind = "absolute-deadline"

	// Ticker receives from a time.Ticker channel.
	//
	// The runtime computes the deadlines, so like AbsoluteDeadline it does not
	// drift. The difference that matters is the buffering: a Ticker's channel
	// holds one tick, so a receiver that falls behind by more than a period
	// silently loses ticks rather than seeing them arrive late. For audio that
	// is a dropped frame the loop never learns about.
	Ticker Kind = "ticker"

	// SpinTail sleeps until shortly before the deadline, then busy-waits.
	//
	// This trades a core for the tail: the sleep covers most of the period, and
	// the spin removes the timer granularity and wakeup latency that dominate
	// the last fraction of a millisecond. It is what a latency-sensitive audio
	// thread does when the platform's timers are too coarse to hit the deadline
	// directly.
	SpinTail Kind = "spin-tail"
)

// Kinds returns every strategy, in the order results should be presented.
func Kinds() []Kind {
	return []Kind{SleepDelta, AbsoluteDeadline, Ticker, SpinTail}
}

// Options configures strategy-specific behaviour.
type Options struct {
	// SpinSlack is how long before the deadline SpinTail stops sleeping and
	// starts busy-waiting. It must exceed the platform's timer granularity
	// plus its wakeup latency, or the sleep overshoots the deadline and the
	// spin never runs. Too large and the loop burns a core for no benefit.
	SpinSlack time.Duration
}

// DefaultOptions returns options suitable for a 20ms period.
//
// The 2ms spin slack is chosen to sit above Windows' default ~15.6ms timer
// granularity being irrelevant only because Go requests a finer resolution,
// and above the ~1ms granularity typical of a default-configured Linux
// kernel. It is a starting point to be tuned against measurements on the
// host, not a universal constant.
func DefaultOptions() Options {
	return Options{SpinSlack: 2 * time.Millisecond}
}

// Pacer blocks until each successive tick is due.
//
// A Pacer never drops ticks: if the loop falls behind, Wait returns
// immediately and lateness shows up in the measurements instead of being
// hidden. The one exception is Ticker, where dropping is the runtime's
// behaviour and not something this package can suppress; that difference is
// itself one of the findings.
type Pacer interface {
	// Wait blocks until the next tick is due and returns when the caller
	// should perform that tick's work.
	Wait()

	// Stop releases any resources held by the strategy.
	Stop()

	// Name reports the strategy, for labelling output.
	Name() string
}

// New builds a Pacer.
//
// origin is the instant tick 0 completed, from which deadline-based
// strategies compute every subsequent deadline. Callers should capture it
// immediately before entering the loop.
func New(kind Kind, period time.Duration, origin time.Time, opts Options) (Pacer, error) {
	if period <= 0 {
		return nil, fmt.Errorf("pacer: period must be positive, got %v", period)
	}

	switch kind {
	case SleepDelta:
		return &sleepDelta{period: period}, nil
	case AbsoluteDeadline:
		return &absoluteDeadline{period: period, origin: origin}, nil
	case Ticker:
		return &ticker{t: time.NewTicker(period)}, nil
	case SpinTail:
		if opts.SpinSlack <= 0 {
			return nil, fmt.Errorf("pacer: %s requires a positive SpinSlack", SpinTail)
		}
		if opts.SpinSlack >= period {
			return nil, fmt.Errorf("pacer: SpinSlack %v must be less than period %v", opts.SpinSlack, period)
		}
		return &spinTail{period: period, origin: origin, slack: opts.SpinSlack}, nil
	default:
		return nil, fmt.Errorf("pacer: unknown strategy %q", kind)
	}
}

type sleepDelta struct {
	period time.Duration
}

func (p *sleepDelta) Wait()        { time.Sleep(p.period) }
func (p *sleepDelta) Stop()        {}
func (p *sleepDelta) Name() string { return string(SleepDelta) }

type absoluteDeadline struct {
	period time.Duration
	origin time.Time
	n      int64
}

func (p *absoluteDeadline) Wait() {
	p.n++

	// Deadlines are multiplied out from the origin rather than accumulated by
	// repeatedly adding period to a running total. In Go both are exact, since
	// a Duration is an integer count of nanoseconds, but the multiplied form
	// is the one that stays correct when this is ported to a language whose
	// clock is a float, which the Python half of this harness is.
	deadline := p.origin.Add(time.Duration(p.n) * p.period)

	if d := time.Until(deadline); d > 0 {
		time.Sleep(d)
	}
	// If d <= 0 the loop is already past this deadline. Returning immediately
	// is deliberate: the tick is reported as late rather than skipped, so the
	// measurement shows the overrun.
}

func (p *absoluteDeadline) Stop()        {}
func (p *absoluteDeadline) Name() string { return string(AbsoluteDeadline) }

type ticker struct {
	t *time.Ticker
}

func (p *ticker) Wait()        { <-p.t.C }
func (p *ticker) Stop()        { p.t.Stop() }
func (p *ticker) Name() string { return string(Ticker) }

type spinTail struct {
	period time.Duration
	origin time.Time
	slack  time.Duration
	n      int64
}

func (p *spinTail) Wait() {
	p.n++
	deadline := p.origin.Add(time.Duration(p.n) * p.period)

	if d := time.Until(deadline) - p.slack; d > 0 {
		time.Sleep(d)
	}

	// Busy-wait the remainder. Since Go 1.14 the scheduler preempts tight
	// loops asynchronously, so this does not starve other goroutines on the
	// same P, but it does occupy a core for the length of the slack window on
	// every tick. That cost is the trade being measured.
	for time.Now().Before(deadline) {
	}
}

func (p *spinTail) Stop()        {}
func (p *spinTail) Name() string { return string(SpinTail) }

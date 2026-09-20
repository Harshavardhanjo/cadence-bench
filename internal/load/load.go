// Package load provides the CPU work and background pressure that a paced
// loop is measured under.
//
// An idle loop on an idle machine holds almost any deadline, so measuring one
// says nothing. The interesting question is what happens to the tail when the
// loop has real work to do and the runtime is busy with something else, which
// is the normal condition for a media server carrying concurrent calls.
package load

import (
	"sync"
	"sync/atomic"
	"time"
)

// sink absorbs the results of otherwise useless arithmetic so the compiler
// cannot delete the busy loops that produce it.
var sink int64

// Spin burns CPU for at least d and returns.
//
// This stands in for per-tick encode work. It spins rather than sleeps because
// encoding a frame consumes a core; a sleep would instead measure the
// platform's timer granularity, which is what the pacer under test is being
// judged on.
func Spin(d time.Duration) {
	if d <= 0 {
		return
	}
	start := time.Now()
	for time.Since(start) < d {
	}
}

// Contention starts n goroutines that consume CPU until the returned stop
// function is called. stop blocks until they have all exited.
//
// This competes with the paced loop for scheduler time. With n at or above the
// number of cores, the loop can no longer assume a core is free at the instant
// its deadline arrives, which is where wakeup latency starts showing up in the
// tail.
func Contention(n int) (stop func()) {
	done := make(chan struct{})
	var wg sync.WaitGroup

	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			// Accumulate locally and publish once. Writing to a shared counter
			// on every iteration would make this a contended-atomic benchmark
			// instead of a CPU one, and would be reported by the race detector.
			var local int64
			for {
				select {
				case <-done:
					atomic.AddInt64(&sink, local)
					return
				default:
				}
				for j := 0; j < 1<<16; j++ {
					local += int64(j)
				}
			}
		}()
	}

	return func() {
		close(done)
		wg.Wait()
	}
}

// Churn allocates and discards bytesPerAlloc-sized buffers until the returned
// stop function is called, creating garbage collector pressure.
//
// A rolling window of allocations is kept reachable so that some survive a
// collection and are promoted, rather than every object dying young where the
// collector handles it most cheaply. The effect being looked for is the pause
// and the assist work landing inside a tick, not the allocation cost itself.
func Churn(bytesPerAlloc int) (stop func()) {
	if bytesPerAlloc <= 0 {
		bytesPerAlloc = 64 << 10
	}

	done := make(chan struct{})
	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()

		const window = 64
		ring := make([][]byte, window)
		i := 0
		for {
			select {
			case <-done:
				// Touch the ring so it cannot be optimised away.
				var n int64
				for _, b := range ring {
					n += int64(len(b))
				}
				atomic.AddInt64(&sink, n)
				return
			default:
			}
			ring[i%window] = make([]byte, bytesPerAlloc)
			i++
		}
	}()

	return func() {
		close(done)
		wg.Wait()
	}
}

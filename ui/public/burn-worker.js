// Occupies one CPU core until terminated.
//
// This is the browser's version of the command line tool's cpu-contention
// scenario. That scenario starts busy goroutines, which cannot work here: Go's
// WebAssembly runtime has one thread and no asynchronous preemption, so a
// goroutine that never blocks would take the thread and never give it back.
// Workers are real threads, so they compete with the measurement for cores the
// way other processes on a media server would.
let sink = 0;
for (;;) {
  for (let i = 0; i < 1e7; i++) sink += i;
  if (sink === -1) postMessage(sink);
}

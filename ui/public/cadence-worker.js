// Runs the Go harness, compiled to WebAssembly, off the main thread.
//
// Two reasons it cannot run on the page's own thread. Spin-tail and the
// per-tick encode work busy-wait, which would freeze the page for the length
// of every run. And the page's thread is the one the browser interrupts for
// rendering, input and layout, so a measurement taken there would mostly be
// measuring React.
//
// Requests are handled one at a time: the Go runtime here has a single thread,
// and two measurements interleaved on it would each see the other as load.
importScripts("wasm_exec.js");

const ready = (async () => {
  const go = new Go();
  let instance;
  try {
    ({ instance } = await WebAssembly.instantiateStreaming(fetch("cadence.wasm"), go.importObject));
  } catch {
    // instantiateStreaming needs the server to send application/wasm.
    const bytes = await (await fetch("cadence.wasm")).arrayBuffer();
    ({ instance } = await WebAssembly.instantiate(bytes, go.importObject));
  }
  // Not awaited: the Go main blocks forever so its exports stay callable.
  go.run(instance);

  const deadline = Date.now() + 10000;
  while (!self.cadence) {
    if (Date.now() > deadline) throw new Error("wasm started but never published the cadence global");
    await new Promise((r) => setTimeout(r, 5));
  }
  return self.cadence;
})();

let queue = Promise.resolve();

self.onmessage = (event) => {
  const { id, fn, arg } = event.data;
  queue = queue.then(async () => {
    try {
      const cadence = await ready;
      if (typeof cadence[fn] !== "function") throw new Error("no such export: " + fn);
      const out = await cadence[fn](JSON.stringify(arg));
      self.postMessage({ id, result: JSON.parse(out) });
    } catch (err) {
      self.postMessage({ id, error: err && err.message ? err.message : String(err) });
    }
  });
};

// Copies vercel.json into the static export so the deployed site is served
// cross-origin isolated. Browsers coarsen performance.now as a Spectre
// mitigation, and isolation is what earns the finer clock. Without it, Chrome
// rounds to 100us and some browsers to a full millisecond. The probe would then
// report that coarser clock correctly, but the page would be measuring the
// hosting configuration rather than the browser.
import { copyFileSync } from "node:fs";
import { resolve } from "node:path";

const ui = resolve(import.meta.dirname, "..");
copyFileSync(resolve(ui, "vercel.json"), resolve(ui, "out", "vercel.json"));
console.log("vercel.json -> out/vercel.json");

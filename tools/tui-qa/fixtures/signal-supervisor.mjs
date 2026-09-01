import fs from "node:fs";
import process from "node:process";

const [runnerModule, treeFixture, marker, ready, resultPath] = process.argv.slice(2);
if (!runnerModule || !treeFixture || !marker || !ready || !resultPath) throw new Error("signal supervisor arguments required");

const { installSignalAbort, runBoundedProcess } = await import(runnerModule);
const controller = new AbortController();
const restoreSignals = installSignalAbort(controller);
const run = runBoundedProcess(process.execPath, [treeFixture, marker], {
  cwd: process.cwd(),
  env: process.env,
  timeoutMs: 30_000,
  cleanupGraceMs: 300,
  signal: controller.signal
});

while (!fs.existsSync(marker)) await new Promise((resolve) => setTimeout(resolve, 10));
fs.writeFileSync(ready, "ready\n", { encoding: "utf8", flag: "wx", mode: 0o600 });
const result = await run;
restoreSignals();
fs.writeFileSync(resultPath, JSON.stringify(result) + "\n", { encoding: "utf8", flag: "wx", mode: 0o600 });

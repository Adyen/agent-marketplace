import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { describe, it, type TestContext } from "node:test";
import { fileURLToPath } from "node:url";

import { DETECTOR_SPECS } from "../../src/detector-specs.js";
import {
  classifyFixtureExit,
  detectsBlankFrame,
  detectsClippedChrome,
  detectsControlInjection,
  harnessRecoveryComplete,
  productRestorationComplete
} from "../../src/detectors.js";
import { runFixtureSession, type FixtureSessionOptions } from "../../src/fixture-session.js";
import { cleanupRegisteredProcess } from "../../src/process.js";
import { environmentForTest, provision } from "../../src/sandbox.js";
import { escapeTerminal } from "../../src/sanitize.js";

const moduleRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..", "..");
const toolRoot = fs.existsSync(path.join(moduleRoot, "fixtures")) ? moduleRoot : path.resolve(moduleRoot, "..");
const fixture = (name: string): string => path.join(toolRoot, "fixtures", name);

const run = async (
  t: TestContext,
  name: string,
  overrides: Partial<FixtureSessionOptions> = {}
) => {
  const sandbox = provision("detector", name.replace(/\.sh$/, ""), 1);
  t.after(() => fs.rmSync(sandbox.root, { recursive: true, force: true }));
  return runFixtureSession({
    file: fixture(name),
    cwd: sandbox.repo,
    env: environmentForTest(sandbox),
    ...overrides
  });
};

describe("controlled broken PTY fixtures", () => {
  it("declares a unique executable fixture for every controlled failure class", () => {
    assert.equal(new Set(DETECTOR_SPECS.map((spec) => spec.id)).size, DETECTOR_SPECS.length);
    assert.deepEqual(new Set(DETECTOR_SPECS.map((spec) => spec.expectedFailureClass)), new Set([
      "BLANK_SCREEN",
      "CLIPPED_CHROME",
      "UNEXPECTED_EXIT",
      "HANG_TIMEOUT",
      "CONTROL_INJECTION",
      "LEAKED_DESCENDANT",
      "PANIC",
      "TERMINAL_RESTORATION",
      "RETURNED_ERROR",
      "SIGKILL"
    ]));
    for (const spec of DETECTOR_SPECS) assert.ok(fs.statSync(fixture(spec.fixture)).mode & 0o111, spec.fixture);
  });

  it("bounds flooding output before append and still cleans the process group", async (t) => {
    await assert.rejects(run(t, "broken-flood.sh", { timeoutMs: 5_000 }), /raw transcript exceeded 10 MiB cap/);
  });

  it("detects actual right-edge clipping only when raw output contains the missing anchor", async (t) => {
    const result = await run(t, "broken-clip.sh", { columns: 40, rows: 8, timeoutMs: 300 });
    assert.equal(result.targetRaw.includes("RIGHT_EDGE_ANCHOR"), true);
    assert.equal(result.targetFrame.join("\n").includes("LEFT_CHROME_ANCHOR"), true);
    assert.equal(detectsClippedChrome(result, "LEFT_CHROME_ANCHOR", "RIGHT_EDGE_ANCHOR"), true);
    assert.equal(harnessRecoveryComplete(result), true);
  });

  it("classifies a timed-out process and proves group cleanup", async (t) => {
    const result = await run(t, "broken-hang.sh", { timeoutMs: 300 });
    assert.equal(result.targetFrame.join("\n").includes("hang fixture running"), true);
    assert.equal(classifyFixtureExit(result), "HANG_TIMEOUT");
    assert.equal(result.cleanup.complete, true, JSON.stringify(result.cleanup));
  });

  it("detects a genuinely blank product frame before harness recovery output", async (t) => {
    const result = await run(t, "broken-blank.sh", { timeoutMs: 300 });
    assert.equal(detectsBlankFrame(result), true);
    assert.equal(result.timedOut, true);
  });

  it("captures raw OSC/bidi injection while its sanitized form is inert", async (t) => {
    const result = await run(t, "broken-inject.sh");
    assert.equal(detectsControlInjection(result), true);
    const sanitized = escapeTerminal(result.targetRaw);
    assert.equal(sanitized.includes(String.fromCharCode(27)), false);
    assert.equal(sanitized.includes("‮"), false);
    assert.equal(classifyFixtureExit(result), null);
  });

  it("classifies an unexpected nonzero exit", async (t) => {
    const result = await run(t, "broken-exit.sh");
    assert.equal(result.exitCode, 2);
    assert.equal(classifyFixtureExit(result), "UNEXPECTED_EXIT");
  });

  it("detects product mode leakage before separately proving harness recovery", async (t) => {
    const result = await run(t, "broken-restore.sh");
    assert.equal(result.targetRaw.includes("\u001b[?1049h"), true);
    assert.equal(result.targetRaw.includes("\u001b[?1049l"), false);
    assert.equal(productRestorationComplete(result), false);
    assert.equal(harnessRecoveryComplete(result), true);
  });

  it("distinguishes a returned error that restored from an unrestored panic", async (t) => {
    const returned = await run(t, "broken-return-error.sh");
    assert.equal(classifyFixtureExit(returned), "RETURNED_ERROR");
    assert.equal(productRestorationComplete(returned), true);
    assert.equal(harnessRecoveryComplete(returned), true);

    const panic = await run(t, "broken-panic.sh");
    assert.equal(classifyFixtureExit(panic), "PANIC");
    assert.equal(productRestorationComplete(panic), false);
    assert.equal(harnessRecoveryComplete(panic), true);
  });

  it("classifies target SIGKILL while proving owned-PTY recovery", async (t) => {
    const result = await run(t, "broken-sigkill.sh", {
      timeoutMs: 2_000,
      signalWhenOutput: { text: "sigkill fixture ready", signal: "SIGKILL" }
    });
    assert.equal(result.timedOut, false);
    assert.equal(classifyFixtureExit(result), "SIGKILL");
    assert.equal(harnessRecoveryComplete(result), true);
  });

  it("detects and reaps a controlled editor child across process-group boundaries", async (t) => {
    const sandbox = provision("detector", "leaked-child", 1);
    const env = environmentForTest(sandbox);
    const pidFile = env.AM_TUI_QA_DESCENDANT_PID_FILE!;
    t.after(async () => {
      if (fs.existsSync(pidFile)) await cleanupRegisteredProcess(pidFile, 100);
      fs.rmSync(sandbox.root, { recursive: true, force: true });
    });
    const result = await runFixtureSession({
      file: fixture("broken-leaked-child.sh"),
      cwd: sandbox.repo,
      env
    });
    const registered = await cleanupRegisteredProcess(pidFile, 300);
    assert.equal(result.cleanup.complete, true, JSON.stringify(result.cleanup));
    assert.equal(registered.complete, true, JSON.stringify(registered));
    assert.equal(result.cleanup.detected.length > 0 || registered.detected, true);
  });
});

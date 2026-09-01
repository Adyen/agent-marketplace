import assert from "node:assert/strict";
import { spawn } from "node:child_process";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import process from "node:process";
import { describe, it, type TestContext } from "node:test";
import { pathToFileURL } from "node:url";

import { cleanupProcessTree } from "../../src/process.js";
import { runBoundedProcess } from "../../src/runner.js";
import { waitForFile, waitForJSONFile } from "../test-utils.js";

interface ProcessTree {
  root: number;
  grouped: number;
  detached: number;
}

const fixture = path.resolve("fixtures", "process-tree.mjs");

const waitForMarker = (marker: string): Promise<ProcessTree> => waitForJSONFile<ProcessTree>(marker);

const processExists = (pid: number): boolean => {
  try {
    process.kill(pid, 0);
    return true;
  } catch (error) {
    if ((error as NodeJS.ErrnoException).code === "ESRCH") return false;
    throw error;
  }
};

const assertTreeGone = (tree: ProcessTree): void => {
  for (const pid of [tree.root, tree.grouped, tree.detached]) {
    assert.equal(processExists(pid), false, `process ${pid} survived cleanup`);
  }
};

const freshMarker = (t: TestContext): string => {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "tui-qa-runner-"));
  t.after(() => fs.rmSync(dir, { recursive: true, force: true }));
  return path.join(dir, "tree.json");
};

describe("runBoundedProcess", () => {
  it("times out and reaps grouped and detached descendants", async (t) => {
    const marker = freshMarker(t);
    const resultPromise = runBoundedProcess(process.execPath, [fixture, marker], {
      cwd: process.cwd(),
      env: process.env,
      timeoutMs: 250,
      cleanupGraceMs: 300
    });
    const tree = await waitForMarker(marker);
    const result = await resultPromise;

    assert.equal(result.timedOut, true);
    assert.equal(result.interrupted, false);
    assert.equal(result.cleanup.complete, true, JSON.stringify(result.cleanup));
    assertTreeGone(tree);
    assert.deepEqual(await cleanupProcessTree(tree.root, tree.root, 50), {
      rootProcess: tree.root,
      processGroup: tree.root,
      detected: [],
      terminated: [],
      remaining: [],
      complete: true
    });
  });

  it("aborts and reaps grouped and detached descendants", async (t) => {
    const marker = freshMarker(t);
    const controller = new AbortController();
    const resultPromise = runBoundedProcess(process.execPath, [fixture, marker], {
      cwd: process.cwd(),
      env: process.env,
      timeoutMs: 5_000,
      cleanupGraceMs: 300,
      signal: controller.signal
    });
    const tree = await waitForMarker(marker);
    controller.abort();
    const result = await resultPromise;

    assert.equal(result.timedOut, false);
    assert.equal(result.interrupted, true);
    assert.equal(result.cleanup.complete, true, JSON.stringify(result.cleanup));
    assertTreeGone(tree);
  });

  it("bounds captured output", async () => {
    const result = await runBoundedProcess(process.execPath, ["-e", "process.stdout.write('x'.repeat(4096))"], {
      cwd: process.cwd(),
      env: process.env,
      timeoutMs: 2_000,
      outputCapBytes: 128
    });

    assert.equal(result.exitCode, 0);
    assert.equal(result.outputTruncated, true);
    assert.match(result.stdout, /TUI-QA TRUNCATED/);
    assert.ok(Buffer.byteLength(result.stdout) < 256);

    const unicode = await runBoundedProcess(process.execPath, ["-e", "process.stdout.write('a😀b')"], {
      cwd: process.cwd(),
      env: process.env,
      timeoutMs: 2_000,
      outputCapBytes: 3
    });
    assert.equal(unicode.stdout.includes("�"), false);
    assert.ok(unicode.stdout.startsWith("a"));
  });
});

describe("installSignalAbort", () => {
  for (const signal of ["SIGINT", "SIGTERM", "SIGHUP"] as const) {
    it(`aborts supervised work and reaps descendants on ${signal}`, async (t) => {
      const dir = fs.mkdtempSync(path.join(os.tmpdir(), "tui-qa-signal-"));
      const marker = path.join(dir, "tree.json");
      const ready = path.join(dir, "ready");
      const resultPath = path.join(dir, "result.json");
      const supervisor = path.resolve("fixtures", "signal-supervisor.mjs");
      const runnerModule = pathToFileURL(path.resolve("dist-tests", "src", "runner.js")).href;
      const child = spawn(process.execPath, [supervisor, runnerModule, fixture, marker, ready, resultPath], {
        cwd: process.cwd(),
        env: process.env,
        stdio: ["ignore", "pipe", "pipe"]
      });
      let stderr = "";
      child.stderr.on("data", (chunk: Buffer) => {
        stderr += chunk.toString("utf8");
      });
      t.after(async () => {
        if (child.exitCode === null && child.signalCode === null) {
          child.kill("SIGKILL");
          await new Promise<void>((resolve) => child.once("close", () => resolve()));
        }
        fs.rmSync(dir, { recursive: true, force: true });
      });

      await waitForFile(ready);
      const tree = await waitForMarker(marker);
      child.kill(signal);
      const exit = await new Promise<{ code: number | null; signal: NodeJS.Signals | null }>((resolve, reject) => {
        const timer = setTimeout(() => reject(new Error(`signal helper hung: ${stderr}`)), 3_000);
        child.once("close", (code, childSignal) => {
          clearTimeout(timer);
          resolve({ code, signal: childSignal });
        });
      });

      assert.deepEqual(exit, { code: 0, signal: null }, stderr);
      const result = JSON.parse(fs.readFileSync(resultPath, "utf8")) as Awaited<ReturnType<typeof runBoundedProcess>>;
      assert.equal(result.interrupted, true);
      assert.equal(result.cleanup.complete, true, JSON.stringify(result.cleanup));
      assertTreeGone(tree);
    });
  }
});

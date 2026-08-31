import assert from "node:assert/strict";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import process from "node:process";
import { fileURLToPath } from "node:url";

import { expect, test } from "@microsoft/tui-test";

import { TARGET_EXIT_BOUNDARY_VERSION } from "../src/protocol.js";
import { escapeTerminal } from "../src/sanitize.js";
import { waitForFile } from "./test-utils.js";

const runDir = fs.mkdtempSync(path.join(os.tmpdir(), "tui-qa-driver-"));
const lifecycleDir = path.join(runDir, "lifecycle");
const activePidFile = path.join(runDir, "active.pid");
const moduleRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const toolRoot = fs.existsSync(path.join(moduleRoot, "dist", "pty-wrapper.js")) ? moduleRoot : path.resolve(moduleRoot, "..", "..");
const wrapper = path.join(toolRoot, "dist", "pty-wrapper.js");
const fixture = path.join(toolRoot, "fixtures", "driver-capabilities.sh");

test.use({
  columns: 80,
  rows: 24,
  env: {
    PATH: process.env.PATH,
    TERM: "xterm-256color",
    LANG: "C.UTF-8",
    AM_TUI_QA_LIFECYCLE_DIR: lifecycleDir,
    AM_TUI_QA_ACTIVE_PID_FILE: activePidFile
  },
  program: {
    file: process.execPath,
    args: [wrapper, "--", fixture]
  }
});

test.afterAll(() => fs.rmSync(runDir, { recursive: true, force: true }));

test("real PTY supports resize, input, mouse, signals, cells, and owned recovery", async ({ terminal }) => {
  await expect(terminal.getByText("capability-ready size=24 80", { strict: false })).toBeVisible({ timeout: 5_000 });
  terminal.resize(100, 30);
  terminal.keyDown();
  terminal.write(`${String.fromCharCode(27)}[200~keyboard paste payload${String.fromCharCode(27)}[201~\n`);
  terminal.mousePress(2, 2);
  terminal.write("\n");

  await expect(terminal.getByText("observed-size=30 100", { strict: false })).toBeVisible({ timeout: 5_000 });

  const visible = terminal.getViewableBuffer().map((row) => row.join("")).join("\n");
  assert.match(visible, /input-hex=\s*1b\s+5b\s+42/);
  assert.match(visible, /input-hex=\s*1b\s+5b\s+3c\s+30\s+3b\s+33\s+3b\s+33\s+4d/);
  assert.match(visible, /6b\s+65\s+79\s+62\s+6f\s+61\s+72\s+64\s+20\s+70\s+61\s+73\s+74\s+65/);
  const sanitized = escapeTerminal(visible);
  assert.equal(sanitized.includes(String.fromCharCode(27)), false);

  await waitForFile(activePidFile);
  const targetPid = Number.parseInt(fs.readFileSync(activePidFile, "utf8").trim(), 10);
  assert.ok(Number.isSafeInteger(targetPid) && targetPid > 1);
  const exited = new Promise<{ exitCode: number; signal?: number }>((resolve) => terminal.onExit(resolve));
  process.kill(targetPid, "SIGTERM");
  const exit = await exited;
  assert.equal(exit.exitCode, 0);

  const files = fs.readdirSync(lifecycleDir).filter((name) => name.startsWith("lifecycle-") && name.endsWith(".json"));
  assert.equal(files.length, 1);
  const lifecycle = JSON.parse(fs.readFileSync(path.join(lifecycleDir, files[0]), "utf8")) as {
    boundaryVersion: number;
    targetAtExit: { probeOK: boolean; canonical: boolean | null; echo: boolean | null };
    harnessRecovery: { applied: boolean; probeOK: boolean; canonical: boolean | null; echo: boolean | null };
  };
  assert.equal(lifecycle.boundaryVersion, TARGET_EXIT_BOUNDARY_VERSION);
  assert.deepEqual(lifecycle.targetAtExit, { probeOK: true, canonical: false, echo: false });
  assert.deepEqual(lifecycle.harnessRecovery, { applied: true, probeOK: true, canonical: true, echo: true });
});

test.describe("termios probe failure", () => {
  const probeDir = path.join(runDir, "probe-failure");
  const probeLifecycle = path.join(probeDir, "lifecycle");
  test.use({
    env: {
      PATH: "",
      TERM: "xterm-256color",
      LANG: "C.UTF-8",
      AM_TUI_QA_LIFECYCLE_DIR: probeLifecycle,
      AM_TUI_QA_ACTIVE_PID_FILE: path.join(probeDir, "active.pid")
    },
    program: {
      file: process.execPath,
      args: [wrapper, "--", "/bin/true"]
    }
  });

  test("fails closed when stty cannot be executed", async ({ terminal }) => {
    const exited = new Promise<{ exitCode: number; signal?: number }>((resolve) => terminal.onExit(resolve));
    assert.equal((await exited).exitCode, 125);
    const files = fs.readdirSync(probeLifecycle).filter((name) => name.startsWith("lifecycle-") && name.endsWith(".json"));
    assert.equal(files.length, 1);
    const lifecycle = JSON.parse(fs.readFileSync(path.join(probeLifecycle, files[0]), "utf8")) as {
      targetAtExit: { probeOK: boolean };
      harnessRecovery: { probeOK: boolean };
    };
    assert.equal(lifecycle.targetAtExit.probeOK, false);
    assert.equal(lifecycle.harnessRecovery.probeOK, false);
  });
});

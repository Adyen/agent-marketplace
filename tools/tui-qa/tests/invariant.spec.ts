import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import process from "node:process";
import { fileURLToPath } from "node:url";

import { expect, test } from "@microsoft/tui-test";

import { invariantFlow } from "../src/invariant-flows.js";
import { sanitizeMultiline } from "../src/sanitize.js";
import { waitForFile } from "./test-utils.js";

const binary = process.env.AM_TUI_QA_BINARY;
const envFile = process.env.AM_TUI_QA_TEST_ENV_FILE;
const artifactDir = process.env.AM_TUI_QA_FLOW_ARTIFACT_DIR;
const flowId = process.env.AM_TUI_QA_FLOW_ID;
const programCwd = process.env.AM_TUI_QA_PROGRAM_CWD;
if (!binary || !envFile || !artifactDir || !flowId || !programCwd) throw new Error("invariant binary, environment, artifact directory, flow ID, and cwd are required");
const flow = invariantFlow(flowId);
if (!flow || flow.driver !== "pty") throw new Error(`unknown PTY invariant flow ${flowId}`);

const moduleRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const toolRoot = fs.existsSync(path.join(moduleRoot, "dist", "pty-wrapper.js")) ? moduleRoot : path.resolve(moduleRoot, "..", "..");
const wrapper = path.join(toolRoot, "dist", "pty-wrapper.js");
const testEnv = JSON.parse(fs.readFileSync(envFile, "utf8")) as Record<string, string>;
const lifecycleDir = path.join(artifactDir, "lifecycle");
const activePidFile = path.join(artifactDir, "active.pid");

const visibleText = (terminal: { getViewableBuffer(): string[][] }): string =>
  terminal.getViewableBuffer().map((row) => row.join("")).join("\n");

const chromeText = (terminal: { getViewableBuffer(): string[][] }): string => {
  const rows = terminal.getViewableBuffer().map((row) => row.join(""));
  return [rows[0] ?? "", rows[1] ?? "", rows.at(-1) ?? ""].join("\n");
};

const waitForExit = async (
  terminal: { exitResult: { exitCode: number; signal?: number } | null },
  timeoutMs = 5_000
): Promise<{ exitCode: number; signal?: number }> => {
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    if (terminal.exitResult) return terminal.exitResult;
    await new Promise((resolve) => setTimeout(resolve, 10));
  }
  throw new Error("invariant process did not exit within the bounded wait");
};

const waitForStableFrame = async (terminal: { getViewableBuffer(): string[][] }, timeoutMs = 5_000): Promise<string> => {
  const deadline = Date.now() + timeoutMs;
  let previous = "";
  let matches = 0;
  while (Date.now() < deadline) {
    const current = visibleText(terminal);
    if (current === previous) matches++;
    else matches = 0;
    if (matches === 2) return current;
    previous = current;
    await new Promise((resolve) => setTimeout(resolve, 50));
  }
  throw new Error("invariant frame did not become stable within the semantic wait");
};

const assertFrame = (terminal: { getViewableBuffer(): string[][] }, visible: string): string => {
  const frame = terminal.getViewableBuffer();
  assert.match(visible, /\S/);
  assert.match(chromeText(terminal), /Scope|Registry|Status/);
  assert.equal(frame.length <= flow.rows, true);
  assert.equal(frame.every((row) => row.length <= flow.columns), true);
  assert.match(frame.at(-1)?.join("") ?? "", /\bq:?\s*quit\b|\bquit\s*:?q\b/i);
  return visible;
};

const writeEvidence = (visible: string, exit: { exitCode: number; signal?: number }): void => {
  fs.mkdirSync(artifactDir, { recursive: true });
  fs.writeFileSync(path.join(artifactDir, "frame.snapshot.txt"), sanitizeMultiline(visible) + "\n", { mode: 0o600 });
  fs.writeFileSync(
    path.join(artifactDir, "driver-log.txt"),
    `${flow.id}: exit=${exit.exitCode} signal=${exit.signal ?? "none"}\n`,
    { mode: 0o600 }
  );
  fs.writeFileSync(
    path.join(artifactDir, "flow-result.json"),
    JSON.stringify({ flowId: flow.id, exitCode: exit.exitCode, signal: exit.signal ?? null }, null, 2) + "\n",
    { mode: 0o600 }
  );
};

test.use({
  columns: flow.columns,
  rows: flow.rows,
  env: {
    ...testEnv,
    ...flow.env,
    AM_TUI_QA_LIFECYCLE_DIR: lifecycleDir,
    AM_TUI_QA_ACTIVE_PID_FILE: activePidFile
  },
  program: {
    file: process.execPath,
    args: [wrapper, "--cwd", programCwd, "--", binary, ...flow.args]
  }
});

test(`${flow.id}: ${flow.description}`, async ({ terminal }) => {
  if (flow.action === "help") {
    await expect(terminal.getByText(/Usage|Available Commands|agent-marketplace/g, { strict: false })).toBeVisible({ timeout: 5_000 });
    const visible = visibleText(terminal);
    const exit = await waitForExit(terminal);
    assert.equal(exit.exitCode, 0);
    assert.equal(visible.includes("Scope"), false);
    writeEvidence(visible, exit);
    return;
  }

  await expect(terminal.getByText(/Scope|Registry|Status/g, { strict: false })).toBeVisible({ timeout: 5_000 });
  await expect(terminal.getByText("alpha-skill", { strict: false })).toBeVisible({ timeout: 5_000 });
  const visible = assertFrame(terminal, await waitForStableFrame(terminal));
  if (flow.action === "quit-ctrl-c") terminal.write(String.fromCharCode(3));
  else if (flow.action === "quit-sigterm") {
    await waitForFile(activePidFile);
    const targetPid = Number(fs.readFileSync(activePidFile, "utf8").trim());
    assert.equal(Number.isSafeInteger(targetPid) && targetPid > 1, true);
    process.kill(targetPid, "SIGTERM");
  } else terminal.write("q");

  const exit = await waitForExit(terminal);
  if (flow.action !== "quit-sigterm") assert.equal(exit.exitCode, 0);
  writeEvidence(visible, exit);
});

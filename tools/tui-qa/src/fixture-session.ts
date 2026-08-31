import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import process from "node:process";
import { fileURLToPath } from "node:url";

import xterm from "@xterm/headless";
import * as pty from "node-pty";

import { cleanupProcessTree, type ProcessCleanupEvidence } from "./process.js";
import { TARGET_EXIT_BOUNDARY_VERSION } from "./protocol.js";
import { splitTargetTrace } from "./trace.js";

const { Terminal } = xterm;
const RAW_CAP_BYTES = 10 * 1024 * 1024;
const wrapper = path.join(path.dirname(fileURLToPath(import.meta.url)), "pty-wrapper.js");

export interface FixtureSessionOptions {
  file: string;
  args?: string[];
  cwd: string;
  env: NodeJS.ProcessEnv;
  columns?: number;
  rows?: number;
  timeoutMs?: number;
  signalWhenOutput?: { text: string; signal: NodeJS.Signals };
}

export interface FixtureLifecycle {
  exitCode: number | null;
  signal: NodeJS.Signals | null;
  targetAtExit: { probeOK: boolean; canonical: boolean | null; echo: boolean | null };
  harnessRecovery: { applied: boolean; probeOK: boolean; canonical: boolean | null; echo: boolean | null };
}

export interface FixtureSessionResult {
  raw: string;
  targetRaw: string;
  harnessRaw: string;
  targetFrame: string[];
  exitCode: number | null;
  signal: number | null;
  timedOut: boolean;
  lifecycle: FixtureLifecycle;
  cleanup: ProcessCleanupEvidence;
}

const stringEnv = (env: NodeJS.ProcessEnv): Record<string, string> =>
  Object.fromEntries(Object.entries(env).filter((entry): entry is [string, string] => entry[1] !== undefined));

const wait = (milliseconds: number): Promise<void> => new Promise((resolve) => setTimeout(resolve, milliseconds));

const waitForTargetPid = async (file: string, timeoutMs: number): Promise<number> => {
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    if (fs.existsSync(file)) {
      const pid = Number(fs.readFileSync(file, "utf8").trim());
      if (Number.isSafeInteger(pid) && pid > 1) return pid;
    }
    await wait(10);
  }
  throw new Error("fixture target PID was not published within the bounded wait");
};

const renderFrame = async (raw: string, columns: number, rows: number): Promise<string[]> => {
  const terminal = new Terminal({ cols: columns, rows, allowProposedApi: true });
  try {
    await new Promise<void>((resolve) => terminal.write(raw, resolve));
    const frame: string[] = [];
    for (let row = 0; row < rows; row++) {
      frame.push(terminal.buffer.active.getLine(row)?.translateToString(true) ?? "");
    }
    return frame;
  } finally {
    terminal.dispose();
  }
};

const readLifecycle = (directory: string): FixtureLifecycle => {
  const files = fs.readdirSync(directory).filter((name) => name.startsWith("lifecycle-") && name.endsWith(".json"));
  if (files.length !== 1) throw new Error(`expected one fixture lifecycle record, found ${files.length}`);
  const value: unknown = JSON.parse(fs.readFileSync(path.join(directory, files[0]), "utf8"));
  if (typeof value !== "object" || value === null) throw new Error("fixture lifecycle record must be an object");
  return value as FixtureLifecycle;
};

export const runFixtureSession = async (options: FixtureSessionOptions): Promise<FixtureSessionResult> => {
  const columns = options.columns ?? 80;
  const rows = options.rows ?? 24;
  const timeoutMs = options.timeoutMs ?? 2_000;
  if (!Number.isSafeInteger(columns) || columns <= 0 || columns > 1_000 ||
      !Number.isSafeInteger(rows) || rows <= 0 || rows > 1_000 ||
      !Number.isSafeInteger(timeoutMs) || timeoutMs <= 0 || timeoutMs > 120_000) {
    throw new Error("invalid fixture PTY limits");
  }

  const runDirectory = fs.mkdtempSync(path.join(os.tmpdir(), "tui-qa-fixture-"));
  const lifecycleDirectory = path.join(runDirectory, "lifecycle");
  const activePidFile = path.join(runDirectory, "active.pid");
  fs.mkdirSync(lifecycleDirectory, { mode: 0o700 });
  let terminal: pty.IPty | undefined;
  let timer: NodeJS.Timeout | undefined;
  let dataDisposable: { dispose(): void } | undefined;
  let exitDisposable: { dispose(): void } | undefined;
  try {
    const env = {
      ...stringEnv(options.env),
      TERM: "xterm-256color",
      AM_TUI_QA_LIFECYCLE_DIR: lifecycleDirectory,
      AM_TUI_QA_ACTIVE_PID_FILE: activePidFile
    };
    terminal = pty.spawn(process.execPath, [wrapper, "--", options.file, ...(options.args ?? [])], {
      name: "xterm-256color",
      cols: columns,
      rows,
      cwd: options.cwd,
      env
    });

    let raw = "";
    let rawBytes = 0;
    let rawOverflow = false;
    let signalTask: Promise<void> | undefined;
    dataDisposable = terminal.onData((data) => {
      if (rawOverflow) return;
      const chunkBytes = Buffer.byteLength(data);
      if (chunkBytes > RAW_CAP_BYTES - rawBytes) {
        rawOverflow = true;
        terminal?.kill("SIGKILL");
        return;
      }
      raw += data;
      rawBytes += chunkBytes;
      if (!signalTask && options.signalWhenOutput && raw.includes(options.signalWhenOutput.text)) {
        signalTask = waitForTargetPid(activePidFile, 1_000).then((pid) => {
          process.kill(pid, options.signalWhenOutput!.signal);
        });
      }
    });
    const exitPromise = new Promise<{ exitCode: number; signal?: number }>((resolve) => {
      exitDisposable = terminal!.onExit(resolve);
    });
    const timeoutPromise = new Promise<"timeout">((resolve) => {
      timer = setTimeout(() => resolve("timeout"), timeoutMs);
    });
    const outcome = await Promise.race([exitPromise, timeoutPromise]);
    const timedOut = outcome === "timeout";
    if (timedOut) {
      const targetPid = await waitForTargetPid(activePidFile, 1_000);
      process.kill(targetPid, "SIGKILL");
    }
    if (signalTask) await signalTask;
    const exit = timedOut
      ? await Promise.race([exitPromise, wait(1_000).then(() => ({ exitCode: -1, signal: 0 }))])
      : outcome;
    if (timer) clearTimeout(timer);
    if (rawOverflow) throw new Error("fixture raw transcript exceeded 10 MiB cap");
    const boundary = `AM_TUI_QA_TARGET_EXIT_BOUNDARY version=${TARGET_EXIT_BOUNDARY_VERSION}`;
    const boundaryDeadline = Date.now() + 500;
    while (!raw.includes(boundary) && Date.now() < boundaryDeadline) await wait(10);
    dataDisposable.dispose();
    dataDisposable = undefined;
    exitDisposable?.dispose();
    exitDisposable = undefined;
    if (!raw.includes(boundary)) throw new Error("fixture target-exit boundary was not flushed within the bounded wait");

    const cleanup = await cleanupProcessTree(terminal.pid, terminal.pid, 300);
    const segments = splitTargetTrace(raw);
    const lifecycle = readLifecycle(lifecycleDirectory);
    return {
      raw,
      targetRaw: segments.target,
      harnessRaw: segments.harness,
      targetFrame: await renderFrame(segments.target, columns, rows),
      exitCode: exit.exitCode,
      signal: exit.signal ?? null,
      timedOut,
      lifecycle,
      cleanup
    };
  } finally {
    if (timer) clearTimeout(timer);
    dataDisposable?.dispose();
    exitDisposable?.dispose();
    let cleanupError: unknown;
    try {
      if (terminal) {
        const cleanup = await cleanupProcessTree(terminal.pid, terminal.pid, 100);
        if (!cleanup.complete) cleanupError = new Error(`fixture PTY cleanup incomplete: ${cleanup.remaining.join(",")}`);
      }
    } catch (error) {
      cleanupError = error;
    }
    fs.rmSync(runDirectory, { recursive: true, force: true });
    if (cleanupError) throw cleanupError;
  }
};

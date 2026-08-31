import { spawn } from "node:child_process";
import process from "node:process";

import { cleanupProcessTree, type ProcessCleanupEvidence } from "./process.js";

const OUTPUT_TRUNCATION = "\n[TUI-QA TRUNCATED: process log capture limit reached]\n";

export interface ProcessRunResult {
  exitCode: number | null;
  signal: NodeJS.Signals | null;
  stdout: string;
  stderr: string;
  timedOut: boolean;
  interrupted: boolean;
  durationMs: number;
  outputTruncated: boolean;
  cleanup: ProcessCleanupEvidence;
  error?: string;
}

export interface RunOptions {
  cwd: string;
  env: NodeJS.ProcessEnv;
  timeoutMs: number;
  outputCapBytes?: number;
  cleanupGraceMs?: number;
  signal?: AbortSignal;
}

export const PROCESS_SHUTDOWN_SIGNALS = ["SIGINT", "SIGTERM", "SIGHUP"] as const;

export const installSignalAbort = (
  controller: AbortController,
  signals: readonly NodeJS.Signals[] = PROCESS_SHUTDOWN_SIGNALS
): (() => void) => {
  const handlers = new Map<NodeJS.Signals, () => void>();
  for (const signal of signals) {
    const handler = (): void => controller.abort(signal);
    handlers.set(signal, handler);
    process.once(signal, handler);
  }
  return () => {
    for (const [signal, handler] of handlers) process.off(signal, handler);
  };
};

const trimIncompleteUTF8 = (value: Buffer): Buffer => {
  let start = value.length - 1;
  while (start >= 0 && (value[start] & 0xc0) === 0x80) start--;
  if (start < 0) return Buffer.alloc(0);
  const lead = value[start];
  const expected = lead >= 0xf0 && lead <= 0xf4 ? 4 : lead >= 0xe0 && lead <= 0xef ? 3 : lead >= 0xc2 && lead <= 0xdf ? 2 : 1;
  return value.length - start < expected ? value.subarray(0, start) : value;
};

class BoundedOutput {
  private readonly chunks: Buffer[] = [];
  private bytes = 0;
  truncated = false;

  constructor(private readonly capBytes: number) {}

  append(chunk: Buffer): void {
    if (this.bytes >= this.capBytes) {
      this.truncated = true;
      return;
    }
    const remaining = this.capBytes - this.bytes;
    const accepted = chunk.subarray(0, Math.min(chunk.length, remaining));
    this.chunks.push(Buffer.from(accepted));
    this.bytes += accepted.length;
    if (accepted.length < chunk.length) this.truncated = true;
  }

  text(): string {
    const bytes = Buffer.concat(this.chunks);
    const value = (this.truncated ? trimIncompleteUTF8(bytes) : bytes).toString("utf8");
    return this.truncated ? value + OUTPUT_TRUNCATION : value;
  }
}

const emptyCleanup = (): ProcessCleanupEvidence => ({
  rootProcess: 0,
  processGroup: 0,
  detected: [],
  terminated: [],
  remaining: [],
  complete: true
});

const waitForClose = async (
  closed: Promise<{ exitCode: number | null; signal: NodeJS.Signals | null }>,
  timeoutMs: number
): Promise<{ result: { exitCode: number | null; signal: NodeJS.Signals | null }; completed: boolean }> =>
  new Promise((resolve) => {
    const timer = setTimeout(() => resolve({ result: { exitCode: null, signal: null }, completed: false }), timeoutMs);
    closed.then((result) => {
      clearTimeout(timer);
      resolve({ result, completed: true });
    });
  });

export const runBoundedProcess = async (file: string, args: string[], options: RunOptions): Promise<ProcessRunResult> => {
  const started = Date.now();
  const outputCap = options.outputCapBytes ?? 1024 * 1024;
  const stdout = new BoundedOutput(outputCap);
  const stderr = new BoundedOutput(outputCap);
  const child = spawn(file, args, {
    cwd: options.cwd,
    env: options.env,
    stdio: ["ignore", "pipe", "pipe"],
    detached: true
  });
  child.stdout.on("data", (chunk: Buffer) => stdout.append(chunk));
  child.stderr.on("data", (chunk: Buffer) => stderr.append(chunk));

  let spawnError: string | undefined;
  child.on("error", (error) => {
    spawnError = error.message;
  });

  const closed = new Promise<{ exitCode: number | null; signal: NodeJS.Signals | null }>((resolve) => {
    child.on("close", (exitCode, signal) => resolve({ exitCode, signal }));
  });
  let resolveStop!: (reason: "timeout" | "interrupt") => void;
  const stopped = new Promise<"timeout" | "interrupt">((resolve) => {
    resolveStop = resolve;
  });
  const timer = setTimeout(() => {
    if (child.exitCode === null && child.signalCode === null) resolveStop("timeout");
  }, options.timeoutMs);
  const abort = (): void => resolveStop("interrupt");
  options.signal?.addEventListener("abort", abort, { once: true });
  if (options.signal?.aborted) abort();

  const outcome = await Promise.race([
    closed.then((result) => ({ type: "closed" as const, result })),
    stopped.then((reason) => ({ type: "stopped" as const, reason }))
  ]);
  clearTimeout(timer);
  options.signal?.removeEventListener("abort", abort);

  let cleanup = emptyCleanup();
  let result: { exitCode: number | null; signal: NodeJS.Signals | null };
  let timedOut = false;
  let interrupted = false;
  let closeError: string | undefined;
  if (outcome.type === "closed") {
    result = outcome.result;
    if (child.pid) cleanup = await cleanupProcessTree(child.pid, child.pid, options.cleanupGraceMs);
  } else {
    timedOut = outcome.reason === "timeout";
    interrupted = outcome.reason === "interrupt";
    if (child.pid) cleanup = await cleanupProcessTree(child.pid, child.pid, options.cleanupGraceMs);
    const close = await waitForClose(closed, options.cleanupGraceMs ?? 250);
    result = close.result;
    if (!close.completed) {
      closeError = "process close event did not arrive after bounded cleanup";
      child.stdout.destroy();
      child.stderr.destroy();
      child.unref();
    }
  }

  const errors = [spawnError, closeError].filter((value): value is string => value !== undefined);
  return {
    ...result,
    stdout: stdout.text(),
    stderr: stderr.text(),
    timedOut,
    interrupted,
    durationMs: Date.now() - started,
    outputTruncated: stdout.truncated || stderr.truncated,
    cleanup,
    ...(errors.length > 0 ? { error: errors.join("; ") } : {})
  };
};

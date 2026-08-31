import path from "node:path";
import process from "node:process";

import { readBoundedPositiveInt } from "./options.js";

export const LIMITS = {
  semanticWaitMs: 5_000,
  longOperationWaitMs: 30_000,
  flowTimeoutMs: 120_000,
  maxRetries: 1,
  transcriptCapBytes: 10 * 1024 * 1024,
  uploadCapBytes: 100 * 1024 * 1024,
  prRetentionDays: 7,
  releaseRetentionDays: 30
} as const;

export const RUN_CLASSES = ["pr", "nightly", "release", "baseline", "local"] as const;
export type RunClass = (typeof RUN_CLASSES)[number];

export interface WorkbenchConfig {
  runId: string;
  runClass: RunClass;
  repoRoot: string;
  binary: string;
  artifactDir: string;
  reportPath: string;
  resultsPath: string;
  columns: number;
  rows: number;
  retries: number;
  timeoutMs: number;
  nativeWarningAcknowledged: boolean;
  rawUploadRequested: boolean;
  syntheticNoSecret: boolean;
  buildIdentityPath?: string;
}

export const defaultRunID = (): string => {
  const stamp = new Date().toISOString().replace(/[-:]/g, "").replace(/\..+/, "Z");
  return `tui-qa-${stamp}-${process.pid}`;
};

const readRetryCount = (value: string | undefined): number => {
  if (value === undefined || value === "") return LIMITS.maxRetries;
  if (!/^[0-9]+$/.test(value)) throw new Error(`AM_TUI_QA_RETRIES must be 0 or 1, got ${JSON.stringify(value)}`);
  const parsed = Number(value);
  if (!Number.isSafeInteger(parsed) || parsed > LIMITS.maxRetries) {
    throw new Error(`AM_TUI_QA_RETRIES must be at most ${LIMITS.maxRetries}, got ${JSON.stringify(value)}`);
  }
  return parsed;
};

const readBoolean = (name: string, value: string | undefined): boolean => {
  if (value === undefined || value === "" || value === "0") return false;
  if (value === "1") return true;
  throw new Error(`${name} must be 0 or 1, got ${JSON.stringify(value)}`);
};

const readRunClass = (value: string | undefined): RunClass => {
  const runClass = value ?? "local";
  if (!RUN_CLASSES.includes(runClass as RunClass)) {
    throw new Error(`unknown AM_TUI_QA_RUN_CLASS ${JSON.stringify(value)}; expected one of ${RUN_CLASSES.join(", ")}`);
  }
  return runClass as RunClass;
};

const within = (candidate: string, root: string): boolean => {
  const relative = path.relative(root, candidate);
  return relative === "" || (!path.isAbsolute(relative) && relative !== ".." && !relative.startsWith(`..${path.sep}`));
};

const artifactPath = (name: string, value: string | undefined, fallback: string, artifactDir: string): string => {
  const resolved = path.resolve(artifactDir, value?.trim() || fallback);
  if (!within(resolved, artifactDir)) throw new Error(`${name} must remain inside AM_TUI_QA_ARTIFACT_DIR`);
  return resolved;
};

export const loadConfig = (env: NodeJS.ProcessEnv = process.env): WorkbenchConfig => {
  const runId = env.AM_TUI_QA_RUN_ID?.trim() || defaultRunID();
  if (!/^[A-Za-z0-9._-]{1,100}$/.test(runId)) {
    throw new Error("AM_TUI_QA_RUN_ID must be 1-100 letters, digits, dots, underscores, or dashes");
  }
  const repoRoot = path.resolve(env.AM_TUI_QA_REPO_ROOT?.trim() || path.resolve(process.cwd(), "..", ".."));
  const artifactDir = path.resolve(
    env.AM_TUI_QA_ARTIFACT_DIR?.trim() || path.join(repoRoot, ".local", "agent-marketplace", "qa", "tui", runId)
  );
  const rawUploadRequested = readBoolean("AM_TUI_QA_RAW_UPLOAD", env.AM_TUI_QA_RAW_UPLOAD);
  const syntheticNoSecret = readBoolean("AM_TUI_QA_SYNTHETIC_NO_SECRET", env.AM_TUI_QA_SYNTHETIC_NO_SECRET);
  if (rawUploadRequested && !syntheticNoSecret) {
    throw new Error("AM_TUI_QA_RAW_UPLOAD requires AM_TUI_QA_SYNTHETIC_NO_SECRET=1");
  }
  const buildIdentity = env.AM_TUI_QA_BUILD_IDENTITY?.trim();
  return {
    runId,
    runClass: readRunClass(env.AM_TUI_QA_RUN_CLASS),
    repoRoot,
    binary: path.resolve(repoRoot, env.AM_TUI_QA_BINARY?.trim() || ".local/agent-marketplace/bin/agent-marketplace"),
    artifactDir,
    reportPath: artifactPath("AM_TUI_QA_REPORT_PATH", env.AM_TUI_QA_REPORT_PATH, path.join(artifactDir, "report.md"), artifactDir),
    resultsPath: artifactPath("AM_TUI_QA_RESULTS_PATH", env.AM_TUI_QA_RESULTS_PATH, path.join(artifactDir, "results.json"), artifactDir),
    columns: readBoundedPositiveInt("AM_TUI_QA_COLUMNS", env.AM_TUI_QA_COLUMNS, 80, 1_000),
    rows: readBoundedPositiveInt("AM_TUI_QA_ROWS", env.AM_TUI_QA_ROWS, 24, 1_000),
    retries: readRetryCount(env.AM_TUI_QA_RETRIES),
    timeoutMs: readBoundedPositiveInt("AM_TUI_QA_TIMEOUT_MS", env.AM_TUI_QA_TIMEOUT_MS, LIMITS.flowTimeoutMs, LIMITS.flowTimeoutMs),
    nativeWarningAcknowledged: readBoolean("AM_TUI_QA_NATIVE_WARNING_ACK", env.AM_TUI_QA_NATIVE_WARNING_ACK),
    rawUploadRequested,
    syntheticNoSecret,
    ...(buildIdentity ? { buildIdentityPath: path.resolve(repoRoot, buildIdentity) } : {})
  };
};

export const retentionDaysFor = (runClass: RunClass): 0 | 7 | 30 =>
  runClass === "pr"
    ? LIMITS.prRetentionDays
    : runClass === "local" || runClass === "baseline"
      ? 0
      : LIMITS.releaseRetentionDays;

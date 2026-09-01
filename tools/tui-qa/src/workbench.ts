import { execFileSync } from "node:child_process";
import fs from "node:fs";
import path from "node:path";
import process from "node:process";
import { fileURLToPath } from "node:url";

import { ArtifactBudget, boundLocalRawTraces, collectAttemptArtifacts, type EvidenceArtifact } from "./artifacts.js";
import { buildReleaseEquivalent, headSha, readBuildIdentity, type BuildIdentity } from "./build.js";
import { LIMITS, retentionDaysFor, type WorkbenchConfig } from "./config.js";
import { harnessRecoveryComplete } from "./detectors.js";
import { readLifecycleRecord, type FixtureLifecycle } from "./fixture-session.js";
import { seedInvariantFixture } from "./invariant-fixture.js";
import { INVARIANT_FLOWS, type InvariantFlowSpec } from "./invariant-flows.js";
import { compareStrings } from "./order.js";
import { TERMINAL_RESET_SEQUENCE } from "./protocol.js";
import {
  generateMarkdownReport,
  newRunResults,
  sha256Value,
  writeResults,
  type AssertionRecord,
  type AttemptRecord,
  type CheckStatus,
  type FailureClass,
  type FlowRecord,
  type HarnessRecoveryRecord,
  type ProductRestorationRecord,
  type RunResults,
  type SideEffectsRecord
} from "./results.js";
import { runBoundedProcess } from "./runner.js";
import { allowedEnv, sanitizeMultiline } from "./sanitize.js";
import { environmentForTest, provision, summarize, type Sandbox, type TreeSummary } from "./sandbox.js";
import type { FlowStatus } from "./status.js";
import { assessProductRestoration, splitTargetTrace } from "./trace.js";
import { sha256File } from "./digest.js";
import { aggregateAttemptStatuses, certifyingRun, isHarnessLaunchFailure, projectBuildIdentity } from "./workbench-policy.js";

const moduleDirectory = path.dirname(fileURLToPath(import.meta.url));
const packageRoot = fs.existsSync(path.join(moduleDirectory, "..", "package.json"))
  ? path.resolve(moduleDirectory, "..")
  : path.resolve(moduleDirectory, "..", "..");
const tuiTestEntry = path.join(packageRoot, "node_modules", "@microsoft", "tui-test", "index.js");
const invariantSpec = path.join(packageRoot, "tests", "invariant.spec.ts");
const CLASSIFIER_VERSION = "invariant-v1";

interface ManagedSummary {
  roots: Record<string, TreeSummary>;
}

interface AttemptOutcome {
  status: Exclude<FlowStatus, "FLAKY" | "SKIP" | "NOT_APPLICABLE">;
  attempt: AttemptRecord;
  assertions: AssertionRecord[];
  sideEffects: SideEffectsRecord;
  evidence: EvidenceArtifact[];
  reason?: string;
}

export interface WorkbenchOptions {
  signal?: AbortSignal;
}

const managedSummary = (sandbox: Sandbox): ManagedSummary => ({
  roots: Object.fromEntries([
    ["workspace", sandbox.workspace],
    ["home", sandbox.home],
    ["xdg-config", sandbox.xdgConfig],
    ["xdg-cache", sandbox.xdgCache],
    ["xdg-data", sandbox.xdgData],
    ["xdg-state", sandbox.xdgState],
    ["logs", sandbox.logs],
    ["backups", sandbox.backups],
    ["ownership", sandbox.ownership],
    ["generated", sandbox.generated]
  ].map(([name, root]) => [name, summarize(root, sha256File, sandbox.root)]))
});

const sideEffects = (before: ManagedSummary, after: ManagedSummary): SideEffectsRecord => {
  const changedPaths: string[] = [];
  let truncated = Object.values(before.roots).some((summary) => summary.truncated) ||
    Object.values(after.roots).some((summary) => summary.truncated);
  for (const root of Object.keys({ ...before.roots, ...after.roots }).sort()) {
    const beforeEntries = { ...(before.roots[root]?.files ?? {}), ...(before.roots[root]?.symlinks ?? {}) };
    const afterEntries = { ...(after.roots[root]?.files ?? {}), ...(after.roots[root]?.symlinks ?? {}) };
    for (const relative of [...new Set([...Object.keys(beforeEntries), ...Object.keys(afterEntries)])].sort()) {
      if (JSON.stringify(beforeEntries[relative]) !== JSON.stringify(afterEntries[relative])) {
        changedPaths.push(`${root}/${relative}`);
      }
      if (changedPaths.length === 1_000) {
        truncated = true;
        break;
      }
    }
    if (truncated && changedPaths.length === 1_000) break;
  }
  return {
    rootClass: "declared-application-roots",
    beforeDigest: sha256Value(before),
    afterDigest: sha256Value(after),
    changedPaths,
    truncated
  };
};

const check = (passed: boolean): CheckStatus => passed ? "PASS" : "FAIL";

const unavailableProductRestoration = (): ProductRestorationRecord => ({
  canonical: "NOT_APPLICABLE",
  echo: "NOT_APPLICABLE",
  alternateScreenExited: "NOT_APPLICABLE",
  cursorVisible: "NOT_APPLICABLE",
  mouseDisabled: "NOT_APPLICABLE",
  bracketedPasteDisabled: "NOT_APPLICABLE",
  complete: false
});

const commandHarnessRecovery = (complete: boolean): HarnessRecoveryRecord => ({
  applied: false,
  canonical: "NOT_APPLICABLE",
  echo: "NOT_APPLICABLE",
  noDescendants: check(complete),
  complete
});

const unavailablePtyHarnessRecovery = (cleanupComplete: boolean): HarnessRecoveryRecord => ({
  applied: false,
  canonical: "BLOCKED",
  echo: "BLOCKED",
  noDescendants: check(cleanupComplete),
  complete: false
});

const noTuiProductRestoration = (raw: string, lifecycle: FixtureLifecycle): ProductRestorationRecord => {
  const target = lifecycle.targetAtExit;
  const modesInactive = !/[?](?:25l|1049h|1000h|1002h|1003h|1006h|2004h)/.test(raw);
  const canonical = target.probeOK && target.canonical === true;
  const echo = target.probeOK && target.echo === true;
  const complete = canonical && echo && modesInactive;
  return {
    canonical: check(canonical),
    echo: check(echo),
    alternateScreenExited: check(modesInactive),
    cursorVisible: check(modesInactive),
    mouseDisabled: check(modesInactive),
    bracketedPasteDisabled: check(modesInactive),
    complete
  };
};

const tuiProductRestoration = (raw: string, lifecycle: FixtureLifecycle): ProductRestorationRecord => {
  const target = lifecycle.targetAtExit;
  const assessment = assessProductRestoration(raw, {
    canonical: target.probeOK ? target.canonical : null,
    echo: target.probeOK ? target.echo : null
  });
  return {
    canonical: check(assessment.canonical),
    echo: check(assessment.echo),
    alternateScreenExited: check(assessment.alternateScreen),
    cursorVisible: check(assessment.cursorVisible),
    mouseDisabled: check(assessment.mouseDisabled),
    bracketedPasteDisabled: check(assessment.bracketedPasteDisabled),
    complete: assessment.complete
  };
};

const ptyHarnessRecovery = (
  harnessRaw: string,
  lifecycle: FixtureLifecycle,
  cleanup: { complete: boolean }
): HarnessRecoveryRecord => {
  const recovery = lifecycle.harnessRecovery;
  const complete = harnessRecoveryComplete({ harnessRaw, lifecycle, cleanup: {
    rootProcess: 0,
    processGroup: 0,
    detected: [],
    terminated: [],
    remaining: [],
    complete: cleanup.complete
  } });
  return {
    applied: recovery.applied,
    canonical: check(recovery.probeOK && recovery.canonical === true),
    echo: check(recovery.probeOK && recovery.echo === true),
    noDescendants: check(cleanup.complete),
    complete
  };
};

const readTrace = async (directory: string): Promise<string> => {
  if (!fs.existsSync(directory)) throw new Error("TUI trace directory missing");
  const { loadTrace } = await import("@microsoft/tui-test/lib/trace/tracer.js");
  let raw = "";
  let count = 0;
  for (const name of fs.readdirSync(directory).sort(compareStrings)) {
    const file = path.join(directory, name);
    const stat = fs.lstatSync(file);
    if (stat.isSymbolicLink()) throw new Error(`TUI trace symlink rejected: ${file}`);
    if (!stat.isFile()) continue;
    const trace = await loadTrace(file);
    for (const point of trace.tracePoints) if ("data" in point) raw += point.data;
    count++;
  }
  if (count !== 1) throw new Error(`expected one TUI trace, found ${count}`);
  return raw;
};

const localRawEvidence = (artifactRoot: string, traceDirectory: string, truncated: boolean): EvidenceArtifact[] => {
  if (!fs.existsSync(traceDirectory)) return [];
  const evidence: EvidenceArtifact[] = [];
  for (const name of fs.readdirSync(traceDirectory).sort(compareStrings)) {
    const file = path.join(traceDirectory, name);
    const stat = fs.lstatSync(file);
    if (stat.isSymbolicLink()) throw new Error(`raw trace symlink rejected: ${file}`);
    if (!stat.isFile()) continue;
    evidence.push({
      path: path.relative(artifactRoot, file).split(path.sep).join("/"),
      sha256: sha256File(file),
      bytes: stat.size,
      kind: "raw-trace",
      sanitized: false,
      sanitization: "raw",
      localOnly: true,
      truncated
    });
  }
  return evidence;
};

const collectEvidence = (
  config: WorkbenchConfig,
  budget: ArtifactBudget,
  source: string,
  flow: InvariantFlowSpec,
  attempt: number,
  transcriptCapBytes: number,
  rawTraceDirectory?: string,
  rawTruncated = false
): EvidenceArtifact[] => {
  const uploaded = collectAttemptArtifacts({
    sourceDir: source,
    destinationDir: path.join(config.artifactDir, "flows", flow.id, `attempt-${attempt}`),
    artifactRoot: config.artifactDir,
    transcriptCapBytes,
    budget,
    rawPolicy: { upload: false, syntheticNoSecret: false }
  });
  const local = rawTraceDirectory ? localRawEvidence(config.artifactDir, rawTraceDirectory, rawTruncated) : [];
  return [...uploaded, ...local].sort((left, right) => compareStrings(left.path, right.path));
};

const runnerEnvironment = (sandbox: Sandbox): NodeJS.ProcessEnv => ({
  PATH: `${path.dirname(process.execPath)}${path.delimiter}/usr/bin${path.delimiter}/bin`,
  HOME: sandbox.home,
  TMPDIR: sandbox.tmp,
  TMP: sandbox.tmp,
  TEMP: sandbox.tmp,
  TERM: "xterm-256color",
  LANG: "C.UTF-8"
});

const failure = (status: AttemptOutcome["status"], reason: string, failureClass: FailureClass): Pick<AttemptOutcome, "status" | "reason"> & { failureClass: FailureClass } => ({
  status,
  reason: sanitizeMultiline(reason),
  failureClass
});

const prepareFixture = async (
  binary: string,
  sandbox: Sandbox,
  env: NodeJS.ProcessEnv,
  signal?: AbortSignal
): Promise<void> => {
  const result = await runBoundedProcess(binary, ["sync"], {
    cwd: sandbox.repo,
    env,
    timeoutMs: LIMITS.longOperationWaitMs,
    cleanupGraceMs: 500,
    outputCapBytes: 1024 * 1024,
    signal
  });
  if (result.timedOut || result.interrupted || result.error || result.exitCode !== 0 || !result.cleanup.complete) {
    throw new Error(`public fixture sync failed: ${result.error ?? result.stderr ?? `exit ${result.exitCode}`}`);
  }
};

const runPtyAttempt = async (
  config: WorkbenchConfig,
  budget: ArtifactBudget,
  binary: string,
  flow: InvariantFlowSpec,
  attemptNumber: number,
  transcriptCapBytes: number,
  signal?: AbortSignal
): Promise<AttemptOutcome> => {
  const sandbox = provision(config.runId, flow.id, attemptNumber);
  const startedAt = new Date().toISOString();
  const started = Date.now();
  let before: ManagedSummary | undefined;
  try {
    const testEnvironment = environmentForTest(sandbox);
    seedInvariantFixture(sandbox, testEnvironment);
    await prepareFixture(binary, sandbox, testEnvironment, signal);
    before = managedSummary(sandbox);
    const source = path.join(sandbox.artifacts, flow.id);
    fs.mkdirSync(source, { recursive: true });
    const environmentFile = path.join(sandbox.root, "test-env.json");
    fs.writeFileSync(environmentFile, JSON.stringify(testEnvironment, null, 2) + "\n", { mode: 0o600 });
    const rawRoot = path.join(config.artifactDir, "raw", flow.id, `attempt-${attemptNumber}`);
    fs.mkdirSync(rawRoot, { recursive: true });
    const result = await runBoundedProcess(process.execPath, [tuiTestEntry, invariantSpec], {
      cwd: packageRoot,
      env: {
        ...runnerEnvironment(sandbox),
        AM_TUI_QA_SUITE: "invariant",
        AM_TUI_QA_BINARY: binary,
        AM_TUI_QA_TEST_ENV_FILE: environmentFile,
        AM_TUI_QA_FLOW_ARTIFACT_DIR: source,
        AM_TUI_QA_ARTIFACT_DIR: rawRoot,
        AM_TUI_QA_FLOW_ID: flow.id,
        AM_TUI_QA_PROGRAM_CWD: sandbox.repo,
        AM_TUI_QA_TIMEOUT_MS: String(config.timeoutMs)
      },
      timeoutMs: config.timeoutMs + 10_000,
      cleanupGraceMs: 500,
      outputCapBytes: 1024 * 1024,
      signal
    });
    const driverLog = path.join(source, "driver-log.txt");
    if (!fs.existsSync(driverLog)) {
      fs.writeFileSync(driverLog, sanitizeMultiline(`${result.stdout}\n${result.stderr}`) + "\n", { mode: 0o600 });
    }
    fs.writeFileSync(path.join(source, "process-cleanup.json"), JSON.stringify(result.cleanup, null, 2) + "\n", { mode: 0o600 });

    const runnerOutput = `${result.stdout}\n${result.stderr}`;
    const launchFailure = isHarnessLaunchFailure(runnerOutput);
    let outcome = result.timedOut
      ? failure("FAIL", "PTY invariant timed out", "HANG_TIMEOUT")
      : result.error || result.interrupted || launchFailure
        ? failure("BLOCKED", result.error ?? (launchFailure ? "PTY target could not be launched" : "PTY invariant interrupted"), "HARNESS_ERROR")
        : result.exitCode !== 0
          ? failure("FAIL", "Microsoft PTY invariant assertion failed", "ASSERTION_FAILED")
          : { status: "PASS" as const, reason: undefined, failureClass: null };
    let productRestoration = unavailableProductRestoration();
    let harnessRecovery = unavailablePtyHarnessRecovery(result.cleanup.complete);
    let rawTraceDirectory: string | undefined;
    let rawTruncated = false;
    try {
      rawTraceDirectory = path.join(rawRoot, "traces");
      rawTruncated = boundLocalRawTraces(rawTraceDirectory, transcriptCapBytes).truncated;
      budget.truncated ||= rawTruncated;
      const raw = await readTrace(rawTraceDirectory);
      const segments = splitTargetTrace(raw);
      const lifecycle = readLifecycleRecord(path.join(source, "lifecycle"));
      productRestoration = flow.action === "help"
        ? noTuiProductRestoration(segments.target, lifecycle)
        : tuiProductRestoration(segments.target, lifecycle);
      harnessRecovery = ptyHarnessRecovery(segments.harness, lifecycle, result.cleanup);
      fs.writeFileSync(path.join(source, "trace-summary.txt"), sanitizeMultiline(segments.target) + "\n", { mode: 0o600 });
      if (outcome.status === "PASS" && !productRestoration.complete) {
        outcome = failure("FAIL", "product terminal restoration failed", "TERMINAL_RESTORATION");
      } else if (outcome.status === "PASS" && !harnessRecovery.complete) {
        outcome = failure("FAIL", "harness process or PTY recovery failed", "LEAKED_DESCENDANT");
      }
    } catch (error) {
      const message = sanitizeMultiline(error instanceof Error ? error.message : String(error));
      fs.writeFileSync(path.join(source, "trace-analysis-error.txt"), message + "\n", { mode: 0o600 });
      if (outcome.status === "PASS") outcome = failure("BLOCKED", message, "HARNESS_ERROR");
    }

    const activePidFile = path.join(source, "active.pid");
    if (fs.existsSync(activePidFile)) fs.rmSync(activePidFile);
    const after = managedSummary(sandbox);
    const evidence = collectEvidence(config, budget, source, flow, attemptNumber, transcriptCapBytes, rawTraceDirectory, rawTruncated);
    const finishedAt = new Date().toISOString();
    const attempt: AttemptRecord = {
      attemptId: `${flow.id}/a${attemptNumber}`,
      attempt: attemptNumber,
      sandboxId: path.basename(sandbox.root),
      startedAt,
      finishedAt,
      status: outcome.status,
      exitCode: result.exitCode,
      signal: result.signal,
      durationMs: Date.now() - started,
      ...(outcome.reason ? { reason: outcome.reason } : {}),
      observedFailureClass: outcome.failureClass,
      productRestoration,
      harnessRecovery
    };
    return {
      status: outcome.status,
      attempt,
      assertions: [{
        id: "invariant-outcome",
        status: outcome.status === "PASS" ? "PASS" : outcome.status === "BLOCKED" ? "BLOCKED" : "FAIL",
        expected: flow.description,
        observed: outcome.reason ?? "passed"
      }],
      sideEffects: sideEffects(before, after),
      evidence,
      ...(outcome.reason ? { reason: outcome.reason } : {})
    };
  } finally {
    fs.rmSync(sandbox.root, { recursive: true, force: true });
  }
};

const runCommandAttempt = async (
  config: WorkbenchConfig,
  budget: ArtifactBudget,
  binary: string,
  flow: InvariantFlowSpec,
  attemptNumber: number,
  signal?: AbortSignal
): Promise<AttemptOutcome> => {
  const sandbox = provision(config.runId, flow.id, attemptNumber);
  const startedAt = new Date().toISOString();
  const started = Date.now();
  try {
    const env = environmentForTest(sandbox);
    seedInvariantFixture(sandbox, env);
    await prepareFixture(binary, sandbox, env, signal);
    if (flow.action === "org-fallback") {
      fs.writeFileSync(path.join(sandbox.home, ".agents", "config.yaml"), "apiVersion: agent-marketplace/v2\nregistries: []\n", { mode: 0o600 });
    }
    const before = managedSummary(sandbox);
    const source = path.join(sandbox.artifacts, flow.id);
    fs.mkdirSync(source, { recursive: true });
    const result = await runBoundedProcess(binary, flow.args, {
      cwd: sandbox.repo,
      env: { ...env, ...flow.env },
      timeoutMs: config.timeoutMs,
      cleanupGraceMs: 500,
      outputCapBytes: 1024 * 1024,
      signal
    });
    const output = `${result.stdout}\n${result.stderr}`;
    let passed = false;
    if (flow.action === "help") {
      passed = result.exitCode === 0 && /Usage|Available Commands/.test(output) && !output.includes("\u001b[?1049h");
    } else if (flow.action === "tui-without-tty") {
      passed = result.exitCode !== null && result.exitCode !== 0 && !result.timedOut &&
        result.durationMs <= LIMITS.semanticWaitMs && /terminal|tty|interactive/i.test(output);
    } else if (flow.action === "org-fallback") {
      passed = result.exitCode === 0 && /factory/i.test(output);
    }
    const outcome = result.timedOut
      ? failure("FAIL", "command invariant timed out", "HANG_TIMEOUT")
      : result.error || result.interrupted
        ? failure("BLOCKED", result.error ?? "command invariant interrupted", "HARNESS_ERROR")
        : passed
          ? { status: "PASS" as const, reason: undefined, failureClass: null }
          : failure("FAIL", "command invariant assertion failed", "ASSERTION_FAILED");
    fs.writeFileSync(path.join(source, "driver-log.txt"), sanitizeMultiline(output) + "\n", { mode: 0o600 });
    fs.writeFileSync(path.join(source, "process-cleanup.json"), JSON.stringify(result.cleanup, null, 2) + "\n", { mode: 0o600 });
    const after = managedSummary(sandbox);
    const evidence = collectEvidence(config, budget, source, flow, attemptNumber, LIMITS.transcriptCapBytes);
    const attempt: AttemptRecord = {
      attemptId: `${flow.id}/a${attemptNumber}`,
      attempt: attemptNumber,
      sandboxId: path.basename(sandbox.root),
      startedAt,
      finishedAt: new Date().toISOString(),
      status: outcome.status,
      exitCode: result.exitCode,
      signal: result.signal,
      durationMs: Date.now() - started,
      ...(outcome.reason ? { reason: outcome.reason } : {}),
      observedFailureClass: outcome.failureClass,
      productRestoration: unavailableProductRestoration(),
      harnessRecovery: commandHarnessRecovery(result.cleanup.complete)
    };
    return {
      status: outcome.status,
      attempt,
      assertions: [{
        id: "invariant-outcome",
        status: outcome.status === "PASS" ? "PASS" : outcome.status === "BLOCKED" ? "BLOCKED" : "FAIL",
        expected: flow.description,
        observed: outcome.reason ?? "passed"
      }],
      sideEffects: sideEffects(before, after),
      evidence,
      ...(outcome.reason ? { reason: outcome.reason } : {})
    };
  } finally {
    fs.rmSync(sandbox.root, { recursive: true, force: true });
  }
};

const skippedOrganizationFlow = (flow: InvariantFlowSpec): FlowRecord => ({
  flowId: flow.id,
  kind: "invariant",
  driver: flow.driver,
  boundary: "non-tty-process",
  required: flow.required,
  status: "NOT_APPLICABLE",
  reason: "organization fallback requires the contained fixed /etc path",
  selectionReason: "required invariant matrix",
  fixtureId: "local-bare-git-registry-v1",
  expectedFailureClass: null,
  terminal: { profileId: "non-tty", columns: flow.columns, rows: flow.rows },
  attempts: [],
  assertions: [],
  sideEffects: {
    rootClass: "declared-application-roots",
    beforeDigest: sha256Value({}),
    afterDigest: sha256Value({}),
    changedPaths: [],
    truncated: false
  },
  evidence: []
});

const interruptedFlow = (flow: InvariantFlowSpec): FlowRecord => ({
  ...skippedOrganizationFlow(flow),
  status: "BLOCKED",
  reason: "run interrupted before flow start"
});

const blockedOutcome = (flow: InvariantFlowSpec, attemptNumber: number, error: unknown): AttemptOutcome => {
  const reason = sanitizeMultiline(error instanceof Error ? error.message : String(error));
  const timestamp = new Date().toISOString();
  const pty = flow.driver === "pty";
  return {
    status: "BLOCKED",
    reason,
    attempt: {
      attemptId: `${flow.id}/a${attemptNumber}`,
      attempt: attemptNumber,
      sandboxId: `${flow.id}-blocked-a${attemptNumber}`,
      startedAt: timestamp,
      finishedAt: timestamp,
      status: "BLOCKED",
      exitCode: null,
      signal: null,
      durationMs: 0,
      reason,
      observedFailureClass: "HARNESS_ERROR",
      productRestoration: unavailableProductRestoration(),
      harnessRecovery: {
        applied: false,
        canonical: pty ? "BLOCKED" : "NOT_APPLICABLE",
        echo: pty ? "BLOCKED" : "NOT_APPLICABLE",
        noDescendants: "BLOCKED",
        complete: false
      }
    },
    assertions: [{ id: "invariant-outcome", status: "BLOCKED", expected: flow.description, observed: reason }],
    sideEffects: {
      rootClass: "declared-application-roots",
      beforeDigest: sha256Value({}),
      afterDigest: sha256Value({}),
      changedPaths: [],
      truncated: false
    },
    evidence: []
  };
};

const runFlow = async (
  config: WorkbenchConfig,
  budget: ArtifactBudget,
  binary: string,
  flow: InvariantFlowSpec,
  signal?: AbortSignal
): Promise<FlowRecord> => {
  if (flow.action === "org-fallback" && process.env.AM_TUI_QA_CONTAINED !== "1") return skippedOrganizationFlow(flow);
  const outcomes: AttemptOutcome[] = [];
  const transcriptCapBytes = Math.floor(LIMITS.transcriptCapBytes / (config.retries + 1));
  const execute = async (attemptNumber: number): Promise<AttemptOutcome> => {
    try {
      return flow.driver === "pty"
        ? await runPtyAttempt(config, budget, binary, flow, attemptNumber, transcriptCapBytes, signal)
        : await runCommandAttempt(config, budget, binary, flow, attemptNumber, signal);
    } catch (error) {
      return blockedOutcome(flow, attemptNumber, error);
    }
  };
  outcomes.push(await execute(1));
  if (outcomes[0].status === "FAIL" && config.retries > 0) outcomes.push(await execute(2));
  const first = outcomes[0];
  const last = outcomes.at(-1)!;
  const status = aggregateAttemptStatuses(outcomes.map((outcome) => outcome.status));
  const evidence = outcomes.flatMap((outcome) => outcome.evidence).sort((left, right) => compareStrings(left.path, right.path));
  return {
    flowId: flow.id,
    kind: "invariant",
    driver: flow.driver,
    boundary: flow.driver === "pty" ? "real-pty-process" : "non-tty-process",
    required: flow.required,
    status,
    ...(status === "PASS" ? {} : { reason: status === "FLAKY" ? "passed only after one fresh-sandbox retry" : last.reason ?? first.reason ?? status }),
    selectionReason: "required invariant matrix",
    fixtureId: "local-bare-git-registry-v1",
    expectedFailureClass: null,
    terminal: {
      profileId: flow.driver === "pty" ? `${flow.columns}x${flow.rows}` : "non-tty",
      columns: flow.columns,
      rows: flow.rows
    },
    attempts: outcomes.map((outcome) => outcome.attempt),
    assertions: [{
      id: "flow-outcome",
      status: status === "PASS" || status === "FLAKY" ? "PASS" : status === "BLOCKED" ? "BLOCKED" : "FAIL",
      expected: flow.description,
      observed: status === "PASS" ? "passed" : status
    }],
    sideEffects: last.sideEffects,
    evidence
  };
};

const resolveBuild = (config: WorkbenchConfig): BuildIdentity => {
  if (config.buildIdentityPath) return readBuildIdentity(config.buildIdentityPath, config.binary);
  return buildReleaseEquivalent(config.repoRoot, config.binary, headSha(config.repoRoot));
};

const gitValue = (repoRoot: string, args: string[], fallback: string): string => {
  try {
    return execFileSync("git", args, { cwd: repoRoot, encoding: "utf8", stdio: ["ignore", "pipe", "ignore"] }).trim() || fallback;
  } catch {
    return fallback;
  }
};

const revisionValue = (name: string, fallback: string): string => {
  const value = process.env[name];
  if (!value) return fallback;
  if (value === "NO_BASE" || /^[0-9a-f]{40,64}$/.test(value)) return value;
  throw new Error(`${name} has invalid revision identity`);
};

const worktreeDirtyValue = (repoRoot: string): boolean => {
  const value = process.env.AM_TUI_QA_WORKTREE_DIRTY;
  if (value === "0") return false;
  if (value === "1") return true;
  if (value) throw new Error("AM_TUI_QA_WORKTREE_DIRTY must be 0 or 1");
  return gitValue(repoRoot, ["status", "--porcelain"], "DIRTY") !== "";
};

const toolVersion = (command: string, args: string[]): string => {
  try {
    return execFileSync(command, args, {
      encoding: "utf8",
      stdio: ["ignore", "pipe", "ignore"],
      timeout: LIMITS.semanticWaitMs
    }).trim() || "unknown";
  } catch {
    return "unknown";
  }
};

export const runWorkbench = async (config: WorkbenchConfig, options: WorkbenchOptions = {}): Promise<RunResults> => {
  if (fs.existsSync(config.resultsPath) || fs.existsSync(config.reportPath)) {
    throw new Error(`refusing to reuse completed TUI QA run ${config.runId}`);
  }
  fs.mkdirSync(config.artifactDir, { recursive: true, mode: 0o700 });
  const worktreeDirty = worktreeDirtyValue(config.repoRoot);
  const head = revisionValue("AM_TUI_QA_HEAD_SHA", gitValue(config.repoRoot, ["rev-parse", "HEAD"], "NO_HEAD"));
  const base = revisionValue("AM_TUI_QA_BASE_SHA", gitValue(config.repoRoot, ["merge-base", "HEAD", "origin/main"], "NO_BASE"));
  const startedAt = new Date().toISOString();
  const started = Date.now();
  const binary = resolveBuild(config);
  const recordedBinary = projectBuildIdentity(binary, config.repoRoot);
  fs.writeFileSync(path.join(config.artifactDir, "build-identity.json"), JSON.stringify(recordedBinary, null, 2) + "\n", { flag: "wx", mode: 0o600 });
  const budget = new ArtifactBudget(LIMITS.uploadCapBytes);
  const flows: FlowRecord[] = [];
  for (const flow of INVARIANT_FLOWS) {
    flows.push(options.signal?.aborted
      ? interruptedFlow(flow)
      : await runFlow(config, budget, config.binary, flow, options.signal));
  }
  const findings = flows
    .filter((flow) => flow.required && (flow.status === "FAIL" || flow.status === "FLAKY"))
    .map((flow) => ({
      severity: "high" as const,
      classification: "product" as const,
      title: `${flow.flowId}: ${flow.reason ?? flow.status}`,
      flowId: flow.flowId,
      issue: "#41"
    }));
  const results = newRunResults({
    runId: config.runId,
    runClass: config.runClass,
    certifying: certifyingRun(process.env.AM_TUI_QA_CONTAINED === "1", worktreeDirty, binary),
    startedAt,
    finishedAt: new Date().toISOString(),
    durationMs: Date.now() - started,
    revision: { base, head, worktreeDirty },
    binary: recordedBinary,
    classifier: {
      version: CLASSIFIER_VERSION,
      input: { requestedFlowIds: INVARIANT_FLOWS.map((flow) => flow.id) },
      selections: flows.map((flow) => ({ flowId: flow.flowId, reason: flow.selectionReason }))
    },
    fixture: { id: "local-bare-git-registry-v1", version: "1", source: "local-synthetic" },
    tools: {
      nodeVersion: process.version,
      npmVersion: toolVersion("npm", ["--version"]),
      driver: { name: "@microsoft/tui-test", version: "0.0.4" }
    },
    environment: allowedEnv(process.env),
    limits: {
      semanticWaitMs: LIMITS.semanticWaitMs,
      longOperationWaitMs: LIMITS.longOperationWaitMs,
      flowTimeoutMs: config.timeoutMs,
      maxRetries: config.retries,
      transcriptCapBytes: LIMITS.transcriptCapBytes,
      uploadCapBytes: LIMITS.uploadCapBytes
    },
    artifactPolicy: {
      retentionDays: retentionDaysFor(config.runClass),
      rawAnsiLocalOnly: true,
      exceptionalRawUpload: config.rawUploadRequested,
      secretScanRequired: true,
      uploadBytes: budget.writtenBytes,
      uploadCapBytes: budget.capBytes,
      truncated: budget.truncated
    },
    flows,
    findings
  });
  writeResults(config.resultsPath, results);
  fs.writeFileSync(config.reportPath, generateMarkdownReport(results), { encoding: "utf8", flag: "wx", mode: 0o600 });
  return results;
};

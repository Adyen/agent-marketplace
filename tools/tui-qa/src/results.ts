import crypto from "node:crypto";
import fs from "node:fs";
import path from "node:path";

import type { EvidenceArtifact } from "./artifacts.js";
import type { BuildIdentity } from "./build.js";
import { retentionDaysFor } from "./config.js";
import { compareStrings } from "./order.js";
import { assertSchemaValid, validateAgainstSchema } from "./schema.js";
import { ALLOWED_ENV_KEYS, markdownCodeFence, markdownInline, sanitizeMultiline, scanValueForSecrets } from "./sanitize.js";
import { aggregate, exitCodeForStatus, type FinalStatus, type FlowResult, type FlowStatus } from "./status.js";

export const RESULTS_SCHEMA_VERSION = 1;
export const FINAL_PRECEDENCE = ["INVALID", "FAIL", "BLOCKED", "PASS", "NOT_APPLICABLE"] as const;
export const FAILURE_CLASSES = [
  "ASSERTION_FAILED",
  "HANG_TIMEOUT",
  "UNEXPECTED_EXIT",
  "CONTROL_INJECTION",
  "TERMINAL_RESTORATION",
  "RETURNED_ERROR",
  "PANIC",
  "SIGKILL",
  "BLANK_SCREEN",
  "LEAKED_DESCENDANT",
  "CLIPPED_CHROME",
  "MISSING_CONTENT",
  "HARNESS_ERROR"
] as const;
export type FailureClass = (typeof FAILURE_CLASSES)[number];
export type CheckStatus = "PASS" | "FAIL" | "BLOCKED" | "NOT_APPLICABLE";

export interface ProductRestorationRecord {
  canonical: CheckStatus;
  echo: CheckStatus;
  alternateScreenExited: CheckStatus;
  cursorVisible: CheckStatus;
  mouseDisabled: CheckStatus;
  bracketedPasteDisabled: CheckStatus;
  complete: boolean;
}

export interface HarnessRecoveryRecord {
  applied: boolean;
  canonical: CheckStatus;
  echo: CheckStatus;
  noDescendants: CheckStatus;
  complete: boolean;
}

export interface AttemptRecord {
  attemptId: string;
  attempt: number;
  sandboxId: string;
  startedAt: string;
  finishedAt: string;
  status: FlowStatus;
  exitCode: number | null;
  signal: string | null;
  durationMs: number;
  reason?: string;
  observedFailureClass: FailureClass | null;
  productRestoration: ProductRestorationRecord;
  harnessRecovery: HarnessRecoveryRecord;
}

export interface AssertionRecord {
  id: string;
  status: "PASS" | "FAIL" | "BLOCKED";
  expected: string;
  observed: string;
}

export interface SideEffectsRecord {
  rootClass: string;
  beforeDigest: string;
  afterDigest: string;
  changedPaths: string[];
  truncated: boolean;
}

export interface FlowRecord {
  flowId: string;
  kind: "invariant" | "detector" | "product";
  driver: "pty" | "command";
  boundary: "real-pty-process" | "non-tty-process";
  required: boolean;
  status: FlowStatus;
  reason?: string;
  selectionReason: string;
  fixtureId: string;
  expectedFailureClass: FailureClass | null;
  terminal: { profileId: string; columns: number; rows: number };
  attempts: AttemptRecord[];
  assertions: AssertionRecord[];
  sideEffects: SideEffectsRecord;
  evidence: EvidenceArtifact[];
}

export interface FindingRecord {
  severity: "critical" | "high" | "medium" | "low";
  classification: "product" | "environment" | "harness" | "coverage";
  title: string;
  flowId?: string;
  issue?: string;
}

export interface RunResults {
  schemaVersion: 1;
  runId: string;
  runClass: "local" | "baseline" | "pr" | "nightly" | "release";
  certifying: boolean;
  invalidReasons: string[];
  startedAt: string;
  finishedAt: string;
  durationMs: number;
  revision: { base: string; head: string; worktreeDirty: boolean };
  binary: BuildIdentity;
  classifier: {
    version: string;
    input: { requestedFlowIds: string[] };
    selections: Array<{ flowId: string; reason: string }>;
  };
  fixture: { id: string; version: string; source: "local-synthetic" };
  tools: {
    nodeVersion: string;
    npmVersion: string;
    driver: { name: "@microsoft/tui-test"; version: "0.0.4" };
  };
  environment: Record<string, string>;
  limits: {
    semanticWaitMs: 5000;
    longOperationWaitMs: 30000;
    flowTimeoutMs: number;
    maxRetries: number;
    transcriptCapBytes: number;
    uploadCapBytes: number;
  };
  artifactPolicy: {
    retentionDays: 0 | 7 | 30;
    rawAnsiLocalOnly: true;
    exceptionalRawUpload: boolean;
    secretScanRequired: true;
    uploadBytes: number;
    uploadCapBytes: number;
    truncated: boolean;
  };
  flows: FlowRecord[];
  findings: FindingRecord[];
  finalDecision: {
    status: FinalStatus;
    exitCode: number;
    precedence: typeof FINAL_PRECEDENCE;
    reasons: string[];
  };
  finalStatus: FinalStatus;
  exitCode: number;
}

const flowToResult = (flow: FlowRecord): FlowResult => ({
  flowId: flow.flowId,
  required: flow.required,
  status: flow.status,
  reason: flow.reason
});

const decisionReasonsFor = (flows: FlowRecord[], status: FinalStatus): string[] => {
  if (status === "NOT_APPLICABLE") return ["no required flow applies"];
  if (status === "PASS") return ["all applicable required flows passed"];
  return flows
    .filter((flow) =>
      flow.required &&
      (status === "FAIL"
        ? flow.status === "FAIL" || flow.status === "FLAKY"
        : flow.status === "BLOCKED" || flow.status === "SKIP")
    )
    .map((flow) => `${flow.flowId}: ${flow.reason ?? flow.status}`)
    .sort();
};

export const newRunResults = (
  input: Omit<RunResults, "schemaVersion" | "invalidReasons" | "finalDecision" | "finalStatus" | "exitCode">,
  options: { invalidReasons?: string[] } = {}
): RunResults => {
  const invalidReasons = [...new Set(options.invalidReasons ?? [])].filter((reason) => reason.length > 0).sort();
  const finalStatus = aggregate(input.flows.map(flowToResult), { invalid: invalidReasons.length > 0 });
  const exitCode = exitCodeForStatus(finalStatus);
  return {
    schemaVersion: RESULTS_SCHEMA_VERSION,
    ...input,
    invalidReasons,
    finalDecision: {
      status: finalStatus,
      exitCode,
      precedence: FINAL_PRECEDENCE,
      reasons: invalidReasons.length > 0 ? invalidReasons : decisionReasonsFor(input.flows, finalStatus)
    },
    finalStatus,
    exitCode
  };
};

const safeEvidencePath = (value: string): boolean =>
  !path.isAbsolute(value) && !value.includes("\\") && !value.split("/").includes("..");

const validTimestampOrder = (startedAt: string, finishedAt: string): boolean => {
  const started = Date.parse(startedAt);
  const finished = Date.parse(finishedAt);
  return Number.isFinite(started) && Number.isFinite(finished) && finished >= started;
};

const productRestorationComplete = (attempt: AttemptRecord): boolean =>
  [
    attempt.productRestoration.canonical,
    attempt.productRestoration.echo,
    attempt.productRestoration.alternateScreenExited,
    attempt.productRestoration.cursorVisible,
    attempt.productRestoration.mouseDisabled,
    attempt.productRestoration.bracketedPasteDisabled
  ].every((status) => status === "PASS");

const harnessRecoveryComplete = (attempt: AttemptRecord): boolean =>
  [attempt.harnessRecovery.canonical, attempt.harnessRecovery.echo, attempt.harnessRecovery.noDescendants]
    .every((status) => status === "PASS");

const firstUnsanitizedString = (value: unknown, location = "$"): string | undefined => {
  if (typeof value === "string") return sanitizeMultiline(value) === value ? undefined : location;
  if (Array.isArray(value)) {
    for (const [index, nested] of value.entries()) {
      const found = firstUnsanitizedString(nested, `${location}[${index}]`);
      if (found) return found;
    }
  } else if (typeof value === "object" && value !== null) {
    for (const [key, nested] of Object.entries(value as Record<string, unknown>)) {
      const found = firstUnsanitizedString(nested, `${location}.${key}`);
      if (found) return found;
    }
  }
  return undefined;
};

export const validateResults = (value: unknown): string[] => {
  const problems = validateAgainstSchema("results.v1.json", value);
  if (problems.length > 0) return problems;
  const results = value as RunResults;
  const unsanitized = firstUnsanitizedString(results);
  if (unsanitized) problems.push(`unsanitized string at ${unsanitized}`);
  const secretFindings = scanValueForSecrets(results);
  if (secretFindings.length > 0) problems.push(`results contain credential-shaped content: ${secretFindings.join(", ")}`);
  const unexpectedEnvironmentKeys = Object.keys(results.environment).filter(
    (key) => !(ALLOWED_ENV_KEYS as readonly string[]).includes(key)
  );
  if (unexpectedEnvironmentKeys.length > 0) {
    problems.push(`environment contains non-allowlisted keys: ${unexpectedEnvironmentKeys.sort().join(", ")}`);
  }
  const expectedRetention = retentionDaysFor(results.runClass);
  if (results.artifactPolicy.retentionDays !== expectedRetention) {
    problems.push(`artifact retention ${results.artifactPolicy.retentionDays} disagrees with run class ${results.runClass}`);
  }

  const recomputed = aggregate(results.flows.map(flowToResult), { invalid: results.invalidReasons.length > 0 });
  if (JSON.stringify(results.invalidReasons) !== JSON.stringify([...results.invalidReasons].sort())) {
    problems.push("invalidReasons must be sorted");
  }
  if (recomputed !== results.finalStatus) problems.push(`finalStatus ${results.finalStatus} disagrees with flows (${recomputed})`);
  const expectedExit = exitCodeForStatus(results.finalStatus);
  if (results.exitCode !== expectedExit) problems.push(`exitCode ${results.exitCode} disagrees with finalStatus ${results.finalStatus}`);
  if (results.finalDecision.status !== results.finalStatus) problems.push("finalDecision.status disagrees with finalStatus");
  if (results.finalDecision.exitCode !== results.exitCode) problems.push("finalDecision.exitCode disagrees with exitCode");
  if (JSON.stringify(results.finalDecision.precedence) !== JSON.stringify(FINAL_PRECEDENCE)) problems.push("finalDecision.precedence changed");
  if (results.invalidReasons.length > 0 && JSON.stringify(results.finalDecision.reasons) !== JSON.stringify(results.invalidReasons)) {
    problems.push("INVALID final-decision reasons disagree with invalidReasons");
  }
  if (!validTimestampOrder(results.startedAt, results.finishedAt)) problems.push("run timestamps are invalid or out of order");
  if (results.certifying && results.binary.buildClass !== "release-equivalent") {
    problems.push("instrumented binary cannot produce certifying evidence");
  }
  if (results.certifying && results.binary.cgoEnabled) problems.push("CGO-enabled binary cannot produce certifying evidence");
  if (results.certifying && results.revision.worktreeDirty) problems.push("dirty worktree cannot produce certifying evidence");

  const flowIds = new Set<string>();
  const evidenceOwners = new Map<string, string>();
  let uploadBytes = 0;
  for (const flow of results.flows) {
    if (flowIds.has(flow.flowId)) problems.push(`duplicate flow ${flow.flowId}`);
    flowIds.add(flow.flowId);
    if (flow.kind === "detector" && flow.expectedFailureClass === null) {
      problems.push(`flow ${flow.flowId}: detector requires expectedFailureClass`);
    }
    if (flow.kind !== "detector" && flow.expectedFailureClass !== null) {
      problems.push(`flow ${flow.flowId}: only detectors may declare expectedFailureClass`);
    }
    if ((flow.status === "PASS" || flow.status === "FLAKY") && flow.evidence.length === 0) {
      problems.push(`flow ${flow.flowId}: PASS/FLAKY requires driver evidence`);
    }
    if ((flow.status === "PASS" || flow.status === "FLAKY") && flow.driver === "pty") {
      const kinds = new Set(flow.evidence.map((item) => item.kind));
      if (!kinds.has("driver-log") || !kinds.has("process-cleanup")) {
        problems.push(`flow ${flow.flowId}: passing PTY flow requires driver-log and process-cleanup evidence`);
      }
    }
    if (flow.status === "FLAKY" &&
        (flow.attempts.length !== 2 || flow.attempts[0].status !== "FAIL" || flow.attempts[1].status !== "PASS")) {
      problems.push(`flow ${flow.flowId}: FLAKY requires failed first attempt and passing only retry`);
    }
    if (flow.status === "PASS" && (flow.attempts.length !== 1 || flow.attempts[0].status !== "PASS")) {
      problems.push(`flow ${flow.flowId}: PASS requires one passing attempt`);
    }
    if (flow.status === "FAIL" && (flow.attempts.length === 0 || flow.attempts.at(-1)?.status !== "FAIL")) {
      problems.push(`flow ${flow.flowId}: FAIL requires a final failed attempt`);
    }
    if ((flow.status === "SKIP" || flow.status === "NOT_APPLICABLE") && flow.attempts.length !== 0) {
      problems.push(`flow ${flow.flowId}: ${flow.status} cannot contain executed attempts`);
    }
    if ((flow.status === "PASS" || flow.status === "FLAKY") && flow.assertions.some((assertion) => assertion.status !== "PASS")) {
      problems.push(`flow ${flow.flowId}: passing flow contains a non-passing assertion`);
    }

    const attemptIds = new Set<string>();
    for (const [index, attempt] of flow.attempts.entries()) {
      if (attemptIds.has(attempt.attemptId)) problems.push(`flow ${flow.flowId}: duplicate attempt ${attempt.attemptId}`);
      attemptIds.add(attempt.attemptId);
      if (attempt.attempt !== index + 1) problems.push(`flow ${flow.flowId}: attempt numbers are not contiguous`);
      if (!validTimestampOrder(attempt.startedAt, attempt.finishedAt)) problems.push(`flow ${flow.flowId}: invalid attempt timestamps`);
      if (["FLAKY", "SKIP", "NOT_APPLICABLE"].includes(attempt.status)) {
        problems.push(`flow ${flow.flowId}: attempt ${attempt.attempt} has invalid terminal status ${attempt.status}`);
      }
      if (attempt.status === "FAIL" && attempt.observedFailureClass === null) {
        problems.push(`flow ${flow.flowId}: failed attempt requires observedFailureClass`);
      }
      if (attempt.productRestoration.complete !== productRestorationComplete(attempt)) {
        problems.push(`flow ${flow.flowId}: product restoration summary disagrees with checks`);
      }
      if (attempt.harnessRecovery.complete !== harnessRecoveryComplete(attempt)) {
        problems.push(`flow ${flow.flowId}: harness recovery summary disagrees with checks`);
      }
      if ((flow.status === "PASS" || flow.status === "FLAKY") && !attempt.harnessRecovery.complete) {
        problems.push(`flow ${flow.flowId}: passing result requires complete harness recovery for every attempt`);
      }
      if (flow.kind !== "detector" && attempt.status === "PASS" && !attempt.productRestoration.complete) {
        problems.push(`flow ${flow.flowId}: passing product attempt requires complete product restoration`);
      }
      if (flow.kind === "detector" && attempt.status === "PASS" && attempt.observedFailureClass !== flow.expectedFailureClass) {
        problems.push(`flow ${flow.flowId}: detector observed ${attempt.observedFailureClass ?? "no failure"}, expected ${flow.expectedFailureClass}`);
      }
      if (flow.kind !== "detector" && attempt.status === "PASS" && attempt.observedFailureClass !== null) {
        problems.push(`flow ${flow.flowId}: passing non-detector recorded a failure class`);
      }
    }
    if (new Set(flow.attempts.map((attempt) => attempt.sandboxId)).size !== flow.attempts.length) {
      problems.push(`flow ${flow.flowId}: retries must use fresh sandboxes`);
    }
    if (flow.sideEffects.changedPaths.some((changedPath) => !safeEvidencePath(changedPath))) {
      problems.push(`flow ${flow.flowId}: unsafe side-effect path`);
    }

    for (const evidence of flow.evidence) {
      const owner = evidenceOwners.get(evidence.path);
      if (owner) problems.push(`evidence path ${evidence.path} is shared by flows ${owner} and ${flow.flowId}`);
      else evidenceOwners.set(evidence.path, flow.flowId);
      if (!safeEvidencePath(evidence.path)) problems.push(`flow ${flow.flowId}: unsafe evidence path`);
      if (evidence.kind === "raw-trace") {
        if (evidence.sanitized || evidence.sanitization !== "raw") {
          problems.push(`flow ${flow.flowId}: raw trace cannot claim sanitization`);
        }
        if (!evidence.localOnly && !results.artifactPolicy.exceptionalRawUpload) {
          problems.push(`flow ${flow.flowId}: uploaded raw trace violates explicit artifact policy`);
        }
      } else if (!evidence.sanitized || evidence.sanitization !== "transformed") {
        problems.push(`flow ${flow.flowId}: non-raw evidence is not transformed`);
      }
      if (!evidence.localOnly) uploadBytes += evidence.bytes;
    }
    const evidencePaths = flow.evidence.map((evidence) => evidence.path);
    if (new Set(evidencePaths).size !== evidencePaths.length) problems.push(`flow ${flow.flowId}: duplicate evidence paths`);
    if (JSON.stringify(evidencePaths) !== JSON.stringify([...evidencePaths].sort())) {
      problems.push(`flow ${flow.flowId}: evidence paths must be sorted`);
    }
  }
  if (uploadBytes !== results.artifactPolicy.uploadBytes) {
    problems.push(`artifactPolicy.uploadBytes ${results.artifactPolicy.uploadBytes} disagrees with evidence (${uploadBytes})`);
  }

  const selected = results.classifier.selections.map((selection) => selection.flowId).sort();
  const recorded = results.flows.map((flow) => flow.flowId).sort();
  if (new Set(selected).size !== selected.length) problems.push("classifier selections contain duplicates");
  if (JSON.stringify(selected) !== JSON.stringify(recorded)) problems.push("classifier selections disagree with recorded flows");
  for (const finding of results.findings) {
    if (finding.flowId && !flowIds.has(finding.flowId)) problems.push(`finding ${finding.title}: unknown flow ${finding.flowId}`);
    if ((finding.severity === "critical" || finding.severity === "high") && finding.classification === "product" && !finding.issue) {
      problems.push(`finding ${finding.title}: Critical/High product finding requires issue ownership`);
    }
  }
  return problems.sort();
};

export const writeResults = (resultsPath: string, results: RunResults): void => {
  assertSchemaValid("results.v1.json", results);
  const problems = validateResults(results);
  if (problems.length > 0) throw new Error(`invalid results: ${problems.join("; ")}`);
  if (fs.existsSync(resultsPath)) throw new Error(`refusing to overwrite immutable results: ${resultsPath}`);
  fs.mkdirSync(path.dirname(resultsPath), { recursive: true });
  const temporary = `${resultsPath}.tmp-${process.pid}-${crypto.randomUUID()}`;
  try {
    fs.writeFileSync(temporary, JSON.stringify(results, null, 2) + "\n", { encoding: "utf8", flag: "wx", mode: 0o600 });
    try {
      fs.linkSync(temporary, resultsPath);
    } catch (error) {
      if ((error as NodeJS.ErrnoException).code === "EEXIST") {
        throw new Error(`refusing to overwrite immutable results: ${resultsPath}`);
      }
      throw error;
    }
  } finally {
    if (fs.existsSync(temporary)) fs.rmSync(temporary);
  }
};

export const readResults = (resultsPath: string): RunResults => {
  if (fs.statSync(resultsPath).size > 10 * 1024 * 1024) throw new Error("results file exceeds 10 MiB limit");
  const parsed: unknown = JSON.parse(fs.readFileSync(resultsPath, "utf8"));
  const problems = validateResults(parsed);
  if (problems.length > 0) throw new Error(`invalid results: ${problems.join("; ")}`);
  return parsed as RunResults;
};

const canonicalize = (value: unknown): unknown => {
  if (Array.isArray(value)) return value.map(canonicalize);
  if (typeof value === "object" && value !== null) {
    return Object.fromEntries(
      Object.entries(value as Record<string, unknown>)
        .sort(([left], [right]) => compareStrings(left, right))
        .map(([key, nested]) => [key, canonicalize(nested)])
    );
  }
  return value;
};

export const sha256Value = (value: unknown): string =>
  crypto.createHash("sha256").update(JSON.stringify(canonicalize(value))).digest("hex");

export const generateMarkdownReport = (results: RunResults): string => {
  const problems = validateResults(results);
  if (problems.length > 0) throw new Error(`cannot render invalid results: ${problems.join("; ")}`);
  const lines = [
    "# TUI QA Report",
    "",
    "## Summary",
    "",
    `- Run ID: ${markdownInline(results.runId)}`,
    `- Run class: ${markdownInline(results.runClass)}`,
    `- Final status: ${results.finalStatus} (exit ${results.exitCode})`,
    `- Certifying: ${results.certifying ? "yes" : "no"}`,
    `- Revision: ${markdownInline(results.revision.base)}..${markdownInline(results.revision.head)}`,
    `- Binary: ${markdownInline(results.binary.path)}`,
    `- Build class: ${results.binary.buildClass}`,
    `- SHA256: \`${results.binary.sha256}\``,
    `- Upload bytes: ${results.artifactPolicy.uploadBytes}/${results.artifactPolicy.uploadCapBytes}`,
    "",
    "## Final decision",
    ""
  ];
  for (const reason of results.finalDecision.reasons) lines.push(`- ${markdownInline(reason)}`);
  lines.push("", "## Flows", "", "| Flow | Required | Status | Profile | Attempts | Evidence | Reason |", "|---|---|---|---|---|---|---|");
  for (const flow of results.flows) {
    lines.push(`| ${markdownInline(flow.flowId)} | ${flow.required ? "yes" : "no"} | ${flow.status} | ${flow.terminal.columns}x${flow.terminal.rows} | ${flow.attempts.length} | ${flow.evidence.length} | ${flow.reason ? markdownInline(flow.reason) : ""} |`);
  }
  lines.push("", "## Findings", "");
  if (results.findings.length === 0) lines.push("None recorded.");
  else {
    for (const finding of results.findings) {
      lines.push(`- **${finding.severity}/${finding.classification}** ${markdownInline(finding.title)}${finding.issue ? ` (${markdownInline(finding.issue)})` : ""}`);
    }
  }
  lines.push("", "## Evidence hashes", "");
  const evidence = results.flows
    .flatMap((flow) => flow.evidence.map((item) => ({ flowId: flow.flowId, ...item })))
    .sort((left, right) => compareStrings(left.path, right.path));
  if (evidence.length === 0) lines.push("None recorded.");
  else {
    for (const item of evidence) {
      lines.push(`- ${markdownInline(item.flowId)} / ${markdownInline(item.path)}: \`${item.sha256}\` (${item.bytes} bytes)`);
    }
  }
  lines.push("", "## Environment (allowlisted)", "", markdownCodeFence(JSON.stringify(results.environment, Object.keys(results.environment).sort(), 2)), "");
  return lines.join("\n");
};

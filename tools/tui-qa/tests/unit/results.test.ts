import assert from "node:assert/strict";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { describe, it, type TestContext } from "node:test";

import { sha256File } from "../../src/digest.js";
import {
  generateMarkdownReport,
  newRunResults,
  readResults,
  sha256Value,
  validateResults,
  writeResults,
  type CheckStatus,
  type FlowRecord,
  type RunResults
} from "../../src/results.js";

const digest = "a".repeat(64);

const temporaryDirectory = (t: TestContext): string => {
  const directory = fs.mkdtempSync(path.join(os.tmpdir(), "tui-qa-results-"));
  t.after(() => fs.rmSync(directory, { recursive: true, force: true }));
  return directory;
};

const productRestoration = (status: CheckStatus = "PASS") => ({
  canonical: status,
  echo: status,
  alternateScreenExited: status,
  cursorVisible: status,
  mouseDisabled: status,
  bracketedPasteDisabled: status,
  complete: status === "PASS"
});

const harnessRecovery = (status: CheckStatus = "PASS") => ({
  applied: status !== "NOT_APPLICABLE",
  canonical: status,
  echo: status,
  noDescendants: status,
  complete: status === "PASS"
});

const flowRecord = (status: FlowRecord["status"], required = true, attempts = 1, evidence = true): FlowRecord => ({
  flowId: "fixture-flow",
  kind: "invariant",
  driver: "pty",
  boundary: "real-pty-process",
  required,
  status,
  selectionReason: "unit fixture selection",
  fixtureId: "fixture-v1",
  expectedFailureClass: null,
  terminal: { profileId: "baseline-80x24", columns: 80, rows: 24 },
  attempts: Array.from({ length: attempts }, (_, index) => {
    const attemptStatus = status === "FLAKY" ? (index === 0 ? "FAIL" : "PASS") : status;
    const checkStatus: CheckStatus = attemptStatus === "PASS" ? "PASS" : "FAIL";
    return {
      attemptId: `fixture-flow/a${index + 1}`,
      attempt: index + 1,
      sandboxId: `sandbox-${index + 1}`,
      startedAt: `2026-08-21T00:00:0${index}Z`,
      finishedAt: `2026-08-21T00:00:0${index + 1}Z`,
      status: attemptStatus,
      exitCode: attemptStatus === "PASS" ? 0 : 1,
      signal: null,
      durationMs: 10,
      observedFailureClass: attemptStatus === "PASS" ? null : "ASSERTION_FAILED" as const,
      productRestoration: productRestoration(checkStatus),
      harnessRecovery: harnessRecovery("PASS")
    };
  }),
  assertions: [{
    id: "driver-outcome",
    status: status === "PASS" || status === "FLAKY" ? "PASS" : status === "BLOCKED" ? "BLOCKED" : "FAIL",
    expected: "flow contract",
    observed: status
  }],
  sideEffects: { rootClass: "workspace", beforeDigest: digest, afterDigest: digest, changedPaths: [], truncated: false },
  evidence: evidence ? [
    {
      path: "flows/fixture-flow/attempt-1/driver-log.txt",
      sha256: digest,
      bytes: 12,
      kind: "driver-log",
      sanitized: true,
      sanitization: "transformed",
      localOnly: false,
      truncated: false
    },
    {
      path: "flows/fixture-flow/attempt-1/process-cleanup.json",
      sha256: digest,
      bytes: 12,
      kind: "process-cleanup",
      sanitized: true,
      sanitization: "transformed",
      localOnly: false,
      truncated: false
    }
  ] : []
});

const baseResults = (flows: FlowRecord[], options: { invalidReasons?: string[]; certifying?: boolean } = {}): RunResults =>
  newRunResults({
    runId: "run-1",
    runClass: "local",
    certifying: options.certifying ?? true,
    startedAt: "2026-08-21T00:00:00Z",
    finishedAt: "2026-08-21T00:00:01Z",
    durationMs: 1_000,
    revision: { base: "NO_BASE", head: "b".repeat(40), worktreeDirty: false },
    binary: {
      path: "<artifact-root>/bin/agent-marketplace",
      sha256: digest,
      buildClass: "release-equivalent",
      cgoEnabled: false,
      goVersion: "go1.26.6",
      goos: "darwin",
      goarch: "arm64",
      command: ["go", "build", "./cmd/agent-marketplace"],
      tags: [],
      ldflags: ["-X main.version=tui-qa"]
    },
    classifier: {
      version: "invariant-v1",
      input: { requestedFlowIds: flows.map((flow) => flow.flowId) },
      selections: flows.map((flow) => ({ flowId: flow.flowId, reason: flow.selectionReason }))
    },
    fixture: { id: "fixture-v1", version: "1", source: "local-synthetic" },
    tools: { nodeVersion: "v24.18.0", npmVersion: "11.16.0", driver: { name: "@microsoft/tui-test", version: "0.0.4" } },
    environment: { TERM: "xterm-256color" },
    limits: {
      semanticWaitMs: 5000,
      longOperationWaitMs: 30000,
      flowTimeoutMs: 120000,
      maxRetries: 1,
      transcriptCapBytes: 10485760,
      uploadCapBytes: 104857600
    },
    artifactPolicy: {
      retentionDays: 0,
      rawAnsiLocalOnly: true,
      exceptionalRawUpload: false,
      secretScanRequired: true,
      uploadBytes: flows.flatMap((flow) => flow.evidence).reduce((sum, evidence) => sum + (evidence.localOnly ? 0 : evidence.bytes), 0),
      uploadCapBytes: 104857600,
      truncated: false
    },
    flows,
    findings: []
  }, { invalidReasons: options.invalidReasons });

describe("newRunResults", () => {
  it("derives deterministic status and exit precedence", () => {
    assert.equal(baseResults([flowRecord("PASS")]).finalStatus, "PASS");
    assert.equal(baseResults([flowRecord("FAIL"), { ...flowRecord("BLOCKED"), flowId: "blocked" }]).exitCode, 1);
    assert.equal(baseResults([flowRecord("BLOCKED")]).exitCode, 2);
    assert.equal(baseResults([]).finalStatus, "NOT_APPLICABLE");
    const invalid = baseResults([flowRecord("PASS")], { invalidReasons: ["classifier invalid"] });
    assert.equal(invalid.exitCode, 3);
    assert.deepEqual(invalid.finalDecision.reasons, invalid.invalidReasons);
  });
});

describe("validateResults", () => {
  it("uses JSON Schema plus semantic validation", () => {
    assert.deepEqual(validateResults(baseResults([flowRecord("PASS")])), []);
    const invalid = baseResults([flowRecord("PASS")]) as RunResults & { surprise?: string };
    invalid.surprise = "not in schema";
    assert.ok(validateResults(invalid).some((problem) => problem.includes("additional properties")));
  });

  it("rejects final status, upload totals, and Markdown disagreement", () => {
    const results = baseResults([flowRecord("FAIL")]);
    results.finalStatus = "PASS";
    results.exitCode = 0;
    results.artifactPolicy.uploadBytes++;
    const problems = validateResults(results);
    assert.ok(problems.some((problem) => problem.includes("final")));
    assert.ok(problems.some((problem) => problem.includes("uploadBytes")));
    assert.throws(() => generateMarkdownReport(results), /invalid results/);
  });

  it("requires exact retained attempts for PASS and FLAKY", () => {
    assert.ok(validateResults(baseResults([flowRecord("FLAKY", true, 1)])).some((problem) => problem.includes("FLAKY")));
    assert.deepEqual(validateResults(baseResults([flowRecord("FLAKY", true, 2)])), []);
    assert.ok(validateResults(baseResults([flowRecord("PASS", true, 2)])).some((problem) => problem.includes("PASS")));
  });

  it("prevents dirty or instrumented builds from certifying", () => {
    const instrumented = baseResults([flowRecord("PASS")]);
    instrumented.binary.buildClass = "instrumented";
    assert.ok(validateResults(instrumented).length > 0);
    const dirty = baseResults([flowRecord("PASS")]);
    dirty.revision.worktreeDirty = true;
    assert.ok(validateResults(dirty).length > 0);
    const cgo = baseResults([flowRecord("PASS")]);
    cgo.binary.cgoEnabled = true;
    assert.ok(validateResults(cgo).length > 0);
    const retention = baseResults([flowRecord("PASS")]);
    retention.runClass = "pr";
    assert.ok(validateResults(retention).some((problem) => problem.includes("retention")));
  });

  it("requires transformed evidence for passing real-PTY flows", () => {
    assert.ok(validateResults(baseResults([flowRecord("PASS", true, 1, false)])).some((problem) => problem.includes("evidence")));
    const flow = flowRecord("PASS");
    flow.evidence[0].sanitized = false;
    flow.evidence[0].sanitization = "raw";
    assert.ok(validateResults(baseResults([flow])).some((problem) => problem.includes("not transformed")));
  });

  it("allows local-only raw evidence but requires policy for upload", () => {
    const flow = flowRecord("PASS");
    flow.evidence.push({
      path: "flows/fixture-flow/attempt-1/traces/raw.trace",
      sha256: digest,
      bytes: 10,
      kind: "raw-trace",
      sanitized: false,
      sanitization: "raw",
      localOnly: true,
      truncated: false
    });
    assert.deepEqual(validateResults(baseResults([flow])), []);
    flow.evidence.at(-1)!.localOnly = false;
    assert.ok(validateResults(baseResults([flow])).some((problem) => problem.includes("uploaded raw trace")));
  });

  it("does not let harness recovery mask failed product restoration", () => {
    const flow = flowRecord("PASS");
    flow.attempts[0].productRestoration = productRestoration("FAIL");
    flow.attempts[0].harnessRecovery = harnessRecovery("PASS");
    assert.ok(validateResults(baseResults([flow])).some((problem) => problem.includes("product restoration")));
  });

  it("requires detectors to observe their declared failure class", () => {
    const detector = flowRecord("PASS");
    detector.kind = "detector";
    detector.expectedFailureClass = "PANIC";
    detector.attempts[0].observedFailureClass = "RETURNED_ERROR";
    assert.ok(validateResults(baseResults([detector])).some((problem) => problem.includes("expected PANIC")));
    detector.attempts[0].observedFailureClass = "PANIC";
    assert.deepEqual(validateResults(baseResults([detector])), []);
  });

  it("rejects unsanitized strings and non-allowlisted environment keys", () => {
    const unsafe = baseResults([flowRecord("PASS")]);
    unsafe.findings.push({ severity: "low", classification: "harness", title: "token=super-secret" });
    assert.ok(validateResults(unsafe).some((problem) => problem.includes("unsanitized string")));
    const environment = baseResults([flowRecord("PASS")]);
    environment.environment.GITHUB_TOKEN = "hidden";
    assert.ok(validateResults(environment).some((problem) => problem.includes("property name")));
  });

  it("requires issue ownership for Critical/High product findings", () => {
    const results = baseResults([flowRecord("PASS")]);
    results.findings.push({ severity: "high", classification: "product", title: "contract failure" });
    assert.ok(validateResults(results).some((problem) => problem.includes("issue ownership")));
  });
});

describe("writeResults/readResults", () => {
  it("writes schema-valid JSON atomically and refuses overwrite", (t) => {
    const directory = temporaryDirectory(t);
    const file = path.join(directory, "results.json");
    const results = baseResults([flowRecord("PASS")]);
    writeResults(file, results);
    assert.deepEqual(readResults(file), results);
    assert.throws(() => writeResults(file, results), /refusing to overwrite/);
  });
});

describe("reporting and digests", () => {
  it("renders deterministic Markdown with escaped structure", () => {
    const flow = { ...flowRecord("FAIL"), reason: "[click](https://evil) `x`" };
    const results = baseResults([flow]);
    const markdown = generateMarkdownReport(results);
    assert.equal(generateMarkdownReport(results), markdown);
    assert.ok(markdown.includes("Final status: FAIL"));
    assert.ok(markdown.includes("\\[click\\]"));
    assert.equal(markdown.includes("super-secret"), false);
  });

  it("digests files and canonical values deterministically", (t) => {
    const directory = temporaryDirectory(t);
    const file = path.join(directory, "x");
    fs.writeFileSync(file, "hello");
    assert.equal(sha256File(file), "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824");
    assert.equal(sha256Value({ b: 2, a: 1 }), sha256Value({ a: 1, b: 2 }));
  });
});

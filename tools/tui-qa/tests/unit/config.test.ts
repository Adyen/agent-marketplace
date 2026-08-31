import assert from "node:assert/strict";
import path from "node:path";
import process from "node:process";
import { describe, it } from "node:test";
import {
  LIMITS,
  loadConfig,
  retentionDaysFor
} from "../../src/config.js";

describe("loadConfig", () => {
  it("uses bounded defaults", () => {
    const config = loadConfig({});
    assert.equal(config.columns, 80);
    assert.equal(config.rows, 24);
    assert.equal(config.retries, LIMITS.maxRetries);
    assert.equal(config.timeoutMs, LIMITS.flowTimeoutMs);
    assert.equal(config.runClass, "local");
    assert.ok(config.resultsPath.endsWith("results.json"));
    assert.ok(config.reportPath.endsWith("report.md"));
  });

  it("resolves the repository root independently of the caller working directory", () => {
    const expected = loadConfig({}).repoRoot;
    const cwd = process.cwd();
    try {
      process.chdir(path.parse(cwd).root);
      assert.equal(loadConfig({}).repoRoot, expected);
    } finally {
      process.chdir(cwd);
    }
  });

  it("rejects non-positive or invalid numbers instead of widening limits", () => {
    assert.throws(() => loadConfig({ AM_TUI_QA_COLUMNS: "0" }), /must be/);
    assert.throws(() => loadConfig({ AM_TUI_QA_ROWS: "abc" }), /positive integer/);
  });

  it("allows zero retries and rejects more than the one-retry contract", () => {
    assert.equal(loadConfig({ AM_TUI_QA_RETRIES: "0" }).retries, 0);
    assert.throws(() => loadConfig({ AM_TUI_QA_RETRIES: "2" }), /at most 1/);
  });

  it("rejects timeouts beyond the 2-minute flow contract", () => {
    assert.throws(() => loadConfig({ AM_TUI_QA_TIMEOUT_MS: "120001" }), /120000/);
  });

  it("rejects unknown run classes, unsafe run IDs, and ambiguous booleans", () => {
    assert.throws(() => loadConfig({ AM_TUI_QA_RUN_CLASS: "everything" }), /unknown AM_TUI_QA_RUN_CLASS/);
    assert.throws(() => loadConfig({ AM_TUI_QA_RUN_ID: "../../escape" }), /RUN_ID/);
    assert.throws(() => loadConfig({ AM_TUI_QA_RAW_UPLOAD: "yes" }), /must be 0 or 1/);
  });

  it("uses explicit run, binary, artifact, and acknowledgement values", () => {
    const config = loadConfig({
      AM_TUI_QA_RUN_ID: "run-1",
      AM_TUI_QA_RUN_CLASS: "pr",
      AM_TUI_QA_BINARY: "/tmp/bin/agent-marketplace",
      AM_TUI_QA_ARTIFACT_DIR: "/tmp/artifacts/run-1",
      AM_TUI_QA_NATIVE_WARNING_ACK: "1"
    });
    assert.equal(config.runId, "run-1");
    assert.equal(config.runClass, "pr");
    assert.equal(config.binary, "/tmp/bin/agent-marketplace");
    assert.equal(config.artifactDir, "/tmp/artifacts/run-1");
    assert.equal(config.nativeWarningAcknowledged, true);
  });

  it("requires synthetic attestation for raw upload and keeps outputs under the artifact root", () => {
    assert.throws(() => loadConfig({ AM_TUI_QA_RAW_UPLOAD: "1" }), /SYNTHETIC_NO_SECRET/);
    assert.throws(
      () => loadConfig({ AM_TUI_QA_ARTIFACT_DIR: "/tmp/artifacts", AM_TUI_QA_RESULTS_PATH: "/tmp/escape.json" }),
      /must remain inside/
    );
  });
});

describe("retentionDaysFor", () => {
  it("uses 7 days for PR runs and 30 days for nightly/release", () => {
    assert.equal(retentionDaysFor("pr"), 7);
    assert.equal(retentionDaysFor("nightly"), 30);
    assert.equal(retentionDaysFor("release"), 30);
  });

  it("marks local/baseline artifacts as non-uploading", () => {
    assert.equal(retentionDaysFor("local"), 0);
    assert.equal(retentionDaysFor("baseline"), 0);
  });
});

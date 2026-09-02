import assert from "node:assert/strict";
import { describe, it } from "node:test";

import {
  validateAgainstSchema,
  validateAllSchemasCompile
} from "../../src/schema.js";

describe("versioned JSON Schemas", () => {
  it("compiles every TUI QA schema", () => {
    assert.deepEqual(validateAllSchemasCompile(), []);
  });

  it("validates the bounded coverage index", () => {
    const coverage = {
      schemaVersion: 1,
      classifierVersion: "future-v1",
      entries: [{
        surface: "tui-entrypoint",
        paths: ["cmd/agent-marketplace/main.go"],
        flowIds: ["launch-bare-tty"],
        reason: "bare invocation selects TUI only on a usable TTY"
      }]
    };
    assert.deepEqual(validateAgainstSchema("coverage-index.v1.json", coverage), []);
    assert.ok(validateAgainstSchema("coverage-index.v1.json", { ...coverage, extra: true }).length > 0);
  });

  it("forbids exploratory output from claiming deterministic status", () => {
    const supplement = {
      schemaVersion: 1,
      runId: "run-1",
      resultsSha256: "a".repeat(64),
      generatedAt: "2026-08-31T00:00:00Z",
      agent: { name: "tui-qa-explorer", model: "test-model" },
      supplementalOnly: true,
      requestedAdditionalFlowIds: [],
      observations: []
    };
    assert.deepEqual(validateAgainstSchema("exploratory-supplement.v1.json", supplement), []);
    assert.ok(validateAgainstSchema("exploratory-supplement.v1.json", { ...supplement, finalStatus: "PASS" }).length > 0);
    assert.ok(validateAgainstSchema("exploratory-supplement.v1.json", {
      ...supplement,
      observations: [{
        id: "escape",
        severity: "low",
        category: "coverage",
        title: "unsafe evidence",
        detail: "path escape",
        evidencePaths: ["../raw.trace"]
      }]
    }).length > 0);
  });
});

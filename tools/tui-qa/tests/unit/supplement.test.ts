import assert from "node:assert/strict";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { describe, it, type TestContext } from "node:test";

import { normalizeSupplement, readSupplement, writeSupplement } from "../../src/supplement.js";

const privateKeyHeader = ["-----BEGIN ", "PRIVATE KEY-----"].join("");

const supplement = () => ({
  schemaVersion: 1,
  runId: "run-1",
  resultsSha256: "a".repeat(64),
  generatedAt: "2026-08-31T00:00:00Z",
  agent: { name: "tui-qa-explorer", model: "test-model" },
  supplementalOnly: true,
  requestedAdditionalFlowIds: ["flow-z", "flow-a"],
  observations: [{
    id: "observation-1",
    severity: "medium",
    category: "usability",
    title: "unsafe\u001b[2J title",
    detail: "token=super-secret",
    evidencePaths: ["flows/z/frame.txt", "flows/a/frame.txt"]
  }]
});

const temporaryDirectory = (t: TestContext): string => {
  const directory = fs.mkdtempSync(path.join(os.tmpdir(), "tui-qa-supplement-"));
  t.after(() => fs.rmSync(directory, { recursive: true, force: true }));
  return directory;
};

describe("exploratory supplement", () => {
  it("sanitizes advisory text and canonicalizes additive arrays", (t) => {
    const directory = temporaryDirectory(t);
    const file = path.join(directory, "supplement.json");
    const written = writeSupplement(file, supplement());
    assert.deepEqual(written.requestedAdditionalFlowIds, ["flow-a", "flow-z"]);
    assert.deepEqual(written.observations[0].evidencePaths, ["flows/a/frame.txt", "flows/z/frame.txt"]);
    assert.equal(written.observations[0].title.includes(String.fromCharCode(27)), false);
    assert.equal(written.observations[0].detail.includes("super-secret"), false);
    assert.deepEqual(readSupplement(file), written);
    assert.throws(() => writeSupplement(file, supplement()), /refusing to overwrite/);
  });

  it("rejects deterministic authority and duplicate observations", () => {
    assert.throws(() => normalizeSupplement({ ...supplement(), finalStatus: "PASS" }), /additional properties/);
    assert.throws(
      () => normalizeSupplement(supplement(), { runId: "other-run", resultsSha256: "a".repeat(64) }),
      /does not match deterministic results binding/
    );
    const duplicate = supplement();
    duplicate.observations.push({ ...duplicate.observations[0] });
    assert.throws(() => normalizeSupplement(duplicate), /observation IDs must be unique/);
    const secret = supplement();
    secret.observations[0].detail = privateKeyHeader;
    assert.throws(() => normalizeSupplement(secret), /credential-shaped content/);
  });

  it("rejects stored output that bypassed sanitization", (t) => {
    const directory = temporaryDirectory(t);
    const file = path.join(directory, "supplement.json");
    fs.writeFileSync(file, JSON.stringify(supplement()), { mode: 0o600 });
    assert.throws(() => readSupplement(file), /not sanitized and canonically ordered/);
  });
});

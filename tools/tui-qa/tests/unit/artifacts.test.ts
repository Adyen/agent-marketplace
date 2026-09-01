import assert from "node:assert/strict";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { describe, it, type TestContext } from "node:test";

import { ArtifactBudget, boundLocalRawTraces, collectAttemptArtifacts } from "../../src/artifacts.js";

const privateKeyBlock = ["-----BEGIN ", "PRIVATE KEY-----\nprivate-body\n-----END ", "PRIVATE KEY-----"].join("");
const bearerValue = `Bearer ${"abcdefghijklmnop"}`;

const temporaryRoot = (t: TestContext, prefix = "tui-qa-artifacts-"): string => {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), prefix));
  t.after(() => fs.rmSync(root, { recursive: true, force: true }));
  return root;
};

const collect = (root: string, budget = new ArtifactBudget(1_024), rawPolicy = { upload: false, syntheticNoSecret: false }) =>
  collectAttemptArtifacts({
    sourceDir: path.join(root, "source"),
    destinationDir: path.join(root, "destination"),
    artifactRoot: root,
    transcriptCapBytes: 96,
    budget,
    rawPolicy
  });

describe("collectAttemptArtifacts", () => {
  it("rejects budgets above the run upload contract", () => {
    assert.throws(() => new ArtifactBudget(100 * 1024 * 1024 + 1), /artifact budget/);
  });

  it("transforms every non-raw artifact and keeps raw traces local by default", (t) => {
    const root = temporaryRoot(t);
    const source = path.join(root, "source");
    fs.mkdirSync(path.join(source, "traces"), { recursive: true });
    fs.writeFileSync(path.join(source, "traces", "raw.trace"), "\u001b[31mraw\u001b[0m");
    fs.writeFileSync(
      path.join(source, "frame.snapshot.txt"),
      `safe\u001b[2J token=super-secret ‮\n${privateKeyBlock}`
    );

    const evidence = collect(root);
    const copied = fs.readFileSync(path.join(root, "destination", "frame.snapshot.txt"), "utf8");
    assert.equal(fs.existsSync(path.join(root, "destination", "traces", "raw.trace")), false);
    assert.equal(copied.includes(String.fromCharCode(27)), false);
    assert.equal(copied.includes("super-secret"), false);
    assert.equal(copied.includes("private-body"), false);
    assert.equal(copied.includes("‮"), false);
    assert.match(copied, /\\e\[2J/);
    assert.match(copied, /token=\*\*\*/);
    assert.deepEqual(evidence.map(({ kind, sanitized, sanitization }) => ({ kind, sanitized, sanitization })), [
      { kind: "sanitized-frame", sanitized: true, sanitization: "transformed" }
    ]);
  });

  it("bounds exceptional synthetic raw traces and marks them unsanitized", (t) => {
    const root = temporaryRoot(t);
    const traces = path.join(root, "source", "traces");
    fs.mkdirSync(traces, { recursive: true });
    fs.writeFileSync(path.join(traces, "raw.trace"), "x".repeat(512));

    const evidence = collect(root, new ArtifactBudget(1_024), { upload: true, syntheticNoSecret: true });
    const copied = path.join(root, "destination", "traces", "raw.trace");
    assert.ok(fs.statSync(copied).size <= 96);
    assert.match(fs.readFileSync(copied, "utf8"), /TUI-QA TRUNCATED/);
    assert.deepEqual(
      { sanitized: evidence[0].sanitized, sanitization: evidence[0].sanitization, truncated: evidence[0].truncated },
      { sanitized: false, sanitization: "raw", truncated: true }
    );
  });

  it("rejects credential-shaped exceptional raw traces", (t) => {
    const root = temporaryRoot(t);
    const traces = path.join(root, "source", "traces");
    fs.mkdirSync(traces, { recursive: true });
    fs.writeFileSync(path.join(traces, "raw.trace"), `Authorization: ${bearerValue}`);
    assert.throws(() => collect(root, new ArtifactBudget(1_024), { upload: true, syntheticNoSecret: true }), /secret scan rejected/);
  });

  it("enforces the aggregate upload budget across attempts", (t) => {
    const root = temporaryRoot(t);
    const budget = new ArtifactBudget(80);
    for (const name of ["one", "two"]) {
      const source = path.join(root, "source");
      const destination = path.join(root, "destination");
      fs.rmSync(source, { recursive: true, force: true });
      fs.mkdirSync(source);
      fs.writeFileSync(path.join(source, `${name}.txt`), name.repeat(100));
      collectAttemptArtifacts({
        sourceDir: source,
        destinationDir: path.join(destination, name),
        artifactRoot: root,
        transcriptCapBytes: 1_024,
        budget,
        rawPolicy: { upload: false, syntheticNoSecret: false }
      });
    }
    assert.ok(budget.writtenBytes <= 80);
    assert.equal(budget.truncated, true);
  });

  it("rejects source and destination symlink escapes", (t) => {
    const root = temporaryRoot(t);
    const outside = temporaryRoot(t, "tui-qa-artifact-outside-");
    const source = path.join(root, "source");
    fs.mkdirSync(source);
    fs.symlinkSync(outside, path.join(source, "escape"));
    assert.throws(() => collect(root), /symlink/);

    fs.rmSync(source, { recursive: true, force: true });
    fs.mkdirSync(source);
    fs.writeFileSync(path.join(source, "safe.txt"), "safe");
    fs.symlinkSync(outside, path.join(root, "destination"));
    assert.throws(() => collect(root), /symlink escape/);
  });
});

describe("boundLocalRawTraces", () => {
  it("truncates retained raw files to the per-flow cap", (t) => {
    const root = temporaryRoot(t, "tui-qa-raw-cap-");
    const traces = path.join(root, "traces");
    fs.mkdirSync(traces);
    fs.writeFileSync(path.join(traces, "a.trace"), "a".repeat(80));
    fs.writeFileSync(path.join(traces, "b.trace"), "b".repeat(80));
    const result = boundLocalRawTraces(traces, 100);
    const retained = fs.readdirSync(traces).reduce((sum, name) => sum + fs.statSync(path.join(traces, name)).size, 0);
    assert.equal(result.truncated, true);
    assert.equal(retained, 100);
    assert.match(fs.readFileSync(path.join(traces, "b.trace"), "utf8"), /TUI-QA TRUNCATED/);
    assert.ok(fs.existsSync(path.join(root, "trace-summary-truncated.txt")));
    fs.writeFileSync(path.join(traces, "c.trace"), "c".repeat(10));
    assert.equal(boundLocalRawTraces(traces, 100).truncated, true);
  });
});

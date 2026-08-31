import assert from "node:assert/strict";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { describe, it, type TestContext } from "node:test";

import {
  buildReleaseEquivalent,
  goModVersion,
  goVersionAtLeast,
  instrumentedIdentity,
  readBuildIdentity
} from "../../src/build.js";

const temporaryDirectory = (t: TestContext): string => {
  const directory = fs.mkdtempSync(path.join(os.tmpdir(), "tui-qa-build-"));
  t.after(() => fs.rmSync(directory, { recursive: true, force: true }));
  return directory;
};

const writeGoMod = (directory: string): void => {
  fs.writeFileSync(path.join(directory, "go.mod"), "module example.com/x\n\ngo 1.26.6\n");
};

describe("goModVersion", () => {
  it("reads the Go version from go.mod", (t) => {
    const directory = temporaryDirectory(t);
    writeGoMod(directory);
    assert.equal(goModVersion(directory), "go1.26.6");
  });

  it("fails closed when go.mod has no go directive", (t) => {
    const directory = temporaryDirectory(t);
    fs.writeFileSync(path.join(directory, "go.mod"), "module example.com/x\n");
    assert.throws(() => goModVersion(directory), /cannot read Go version/);
  });

  it("accepts compatible newer compilers and rejects older ones", () => {
    assert.equal(goVersionAtLeast("go1.26.7", "go1.26.6"), true);
    assert.equal(goVersionAtLeast("go1.27", "go1.26.6"), true);
    assert.equal(goVersionAtLeast("go1.26.5", "go1.26.6"), false);
    assert.throws(() => goVersionAtLeast("devel", "go1.26.6"), /invalid Go version/);
  });
});

describe("build identity", () => {
  it("marks non-release builds as instrumented", (t) => {
    const directory = temporaryDirectory(t);
    const binary = path.join(directory, "bin");
    fs.writeFileSync(binary, "binary-bytes");
    const identity = instrumentedIdentity(binary, "go1.26.6", "linux", "amd64", ["go", "build", "-tags", "e2e"]);
    assert.equal(identity.buildClass, "instrumented");
    assert.match(identity.sha256, /^[0-9a-f]{64}$/);
    assert.deepEqual(identity.command, ["go", "build", "-tags", "e2e"]);
  });

  it("rejects malformed release revision identity before invoking Go", (t) => {
    const directory = temporaryDirectory(t);
    assert.throws(() => buildReleaseEquivalent(directory, path.join(directory, "bin"), "not-a-sha"), /head SHA/);
  });

  it("verifies a mounted manifest against the binary digest", (t) => {
    const directory = temporaryDirectory(t);
    const binary = path.join(directory, "agent-marketplace");
    fs.writeFileSync(binary, "binary-bytes");
    const identity = instrumentedIdentity(binary, "go1.26.6", "linux", "arm64", ["go", "build"], ["e2e"], []);
    const manifest = path.join(directory, "build-identity.json");
    fs.writeFileSync(manifest, JSON.stringify({ ...identity, surprise: "discarded" }));
    const projected = readBuildIdentity(manifest, binary) as typeof identity & { surprise?: string };
    assert.equal(projected.sha256, identity.sha256);
    assert.equal(projected.surprise, undefined);
    fs.writeFileSync(manifest, JSON.stringify({ ...identity, command: [] }));
    assert.throws(() => readBuildIdentity(manifest, binary), /command invalid/);
    fs.writeFileSync(manifest, JSON.stringify(identity));
    fs.writeFileSync(binary, "changed");
    assert.throws(() => readBuildIdentity(manifest, binary), /digest mismatch/);
    fs.writeFileSync(manifest, "null\n");
    assert.throws(() => readBuildIdentity(manifest, binary), /must be an object/);
  });
});

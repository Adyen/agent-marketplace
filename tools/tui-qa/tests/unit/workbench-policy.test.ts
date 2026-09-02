import assert from "node:assert/strict";
import { describe, it } from "node:test";

import { aggregateAttemptStatuses, certifyingRun, isHarnessLaunchFailure, projectBuildIdentity } from "../../src/workbench-policy.js";
import type { BuildIdentity } from "../../src/build.js";

const revision = { base: "b".repeat(40), head: "c".repeat(40) };

const identity: BuildIdentity = {
  path: "/private/run/agent-marketplace",
  sha256: "a".repeat(64),
  buildClass: "release-equivalent",
  cgoEnabled: false,
  goVersion: "go1.26.6",
  goos: "linux",
  goarch: "arm64",
  command: ["go", "build", "-o", "/private/run/agent-marketplace", "./cmd/agent-marketplace"],
  tags: [],
  ldflags: []
};

describe("workbench policy", () => {
  it("preserves failures across retries and marks pass-on-retry flaky", () => {
    assert.equal(aggregateAttemptStatuses(["PASS"]), "PASS");
    assert.equal(aggregateAttemptStatuses(["FAIL", "PASS"]), "FLAKY");
    assert.equal(aggregateAttemptStatuses(["FAIL", "BLOCKED"]), "FAIL");
    assert.equal(aggregateAttemptStatuses(["BLOCKED"]), "BLOCKED");
  });

  it("projects host build paths out of stored identity", () => {
    assert.deepEqual(projectBuildIdentity(identity, "/private/run"), {
      ...identity,
      path: "<run-binary>/agent-marketplace",
      command: ["go", "build", "-o", "<run-binary>/agent-marketplace", "./cmd/agent-marketplace"]
    });
  });

  it("classifies wrapper configuration and spawn errors as harness launch failures", () => {
    assert.equal(isHarnessLaunchFailure("pty-wrapper: target cwd unavailable"), true);
    assert.equal(isHarnessLaunchFailure("Error: spawn /opt/agent-marketplace EACCES"), true);
    assert.equal(isHarnessLaunchFailure("assertion failed: expected chrome"), false);
  });

  it("certifies only clean contained release-equivalent builds", () => {
    assert.equal(certifyingRun(true, false, identity, revision), true);
    assert.equal(certifyingRun(false, false, identity, revision), false);
    assert.equal(certifyingRun(true, true, identity, revision), false);
    assert.equal(certifyingRun(true, false, { ...identity, buildClass: "instrumented" }, revision), false);
    assert.equal(certifyingRun(true, false, { ...identity, cgoEnabled: true }, revision), false);
    assert.equal(certifyingRun(true, false, identity, { ...revision, base: "NO_BASE" }), false);
  });
});

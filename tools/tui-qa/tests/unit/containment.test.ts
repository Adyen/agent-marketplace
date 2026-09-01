import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { describe, it } from "node:test";
import { fileURLToPath } from "node:url";

const moduleRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..", "..");
const toolRoot = fs.existsSync(path.join(moduleRoot, "selftest-containment.sh")) ? moduleRoot : path.resolve(moduleRoot, "..");
const scriptPath = (name: string): string => path.join(toolRoot, name);
const read = (name: string): string => fs.readFileSync(scriptPath(name), "utf8");

describe("containment self-test", () => {
  it("uses a disposable least-privilege runtime boundary", () => {
    const runner = read("run-containment-selftest.sh");
    for (const required of [
      "docker run --rm --init",
      "--network none",
      "--read-only",
      "--cap-drop ALL",
      "--security-opt no-new-privileges",
      "--pids-limit 64",
      "$root_dir:/src:ro",
      "$tmp_root/artifacts:/qa/artifacts:rw",
      "$tmp_root/agent-marketplace:/opt/agent-marketplace:ro"
    ]) {
      assert.ok(runner.includes(required), required);
    }
  });

  it("runs the release binary in the disposable contained workbench", () => {
    const runner = read("run-container.sh");
    for (const required of [
      "--network none",
      "--read-only",
      "--cap-drop ALL",
      "--security-opt no-new-privileges",
      "--pids-limit 512",
      "/sandbox:rw,nosuid,nodev,exec",
      "/workbench/.tui-test:rw,nosuid,nodev,noexec",
      "$root_dir:/src:ro",
      "$out_dir/artifacts:/qa/artifacts:rw",
      "$out_dir/bin/agent-marketplace:/opt/agent-marketplace:ro",
      "$out_dir/bin/build-identity.json:/opt/build-identity.json:ro"
    ]) {
      assert.ok(runner.includes(required), required);
    }
    assert.equal(runner.includes("/etc/agent-marketplace:rw"), false);
    const dockerfile = read("Dockerfile");
    assert.ok(dockerfile.includes("oracle-10-base-image@sha256:"));
    assert.ok(dockerfile.includes("ARG NODE_VERSION=24.18.0"));
    assert.ok(dockerfile.includes("/etc/agent-marketplace/config.yaml"));
    const entrypointLines = read("container-entrypoint.sh").split("\n");
    const selftestIndex = entrypointLines.findIndex((line) => line.startsWith("if /workbench/selftest-containment.sh "));
    const workbenchIndex = entrypointLines.findIndex((line) => line === "exec node dist/run-workbench.js");
    assert.ok(selftestIndex >= 0);
    assert.ok(workbenchIndex >= 0);
    assert.ok(selftestIndex < workbenchIndex);
  });

  it("fails when required write and network probes cannot run", () => {
    const selftest = read("selftest-containment.sh");
    for (const required of [
      "/etc/containment-probe",
      "/usr/local/bin/containment-probe",
      "/src/containment-probe",
      "/opt/agent-marketplace",
      "writable QA artifact mount",
      "curl missing; network denial cannot be tested",
      "runtime network denied"
    ]) {
      assert.ok(selftest.includes(required), required);
    }
    assert.equal(selftest.includes("sh -c"), false);
  });

  it("keeps runtime scripts executable", () => {
    for (const name of ["run-containment-selftest.sh", "selftest-containment.sh", "run-container.sh", "container-entrypoint.sh"]) {
      assert.ok(fs.statSync(scriptPath(name)).mode & 0o111, name);
    }
  });
});

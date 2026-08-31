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

  it("keeps both scripts executable", () => {
    assert.ok(fs.statSync(scriptPath("run-containment-selftest.sh")).mode & 0o111);
    assert.ok(fs.statSync(scriptPath("selftest-containment.sh")).mode & 0o111);
  });
});

import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import process from "node:process";
import { describe, it } from "node:test";

import { cleanupRegisteredProcess } from "../../src/process.js";
import { runBoundedProcess } from "../../src/runner.js";
import { environmentForTest, provision } from "../../src/sandbox.js";
import { waitForJSONFile } from "../test-utils.js";

interface RegisteredIdentity {
  pid: number;
  pgid: number;
}

const processExists = (pid: number): boolean => {
  try {
    process.kill(pid, 0);
    return true;
  } catch (error) {
    if ((error as NodeJS.ErrnoException).code === "ESRCH") return false;
    throw error;
  }
};

describe("cleanupRegisteredProcess", () => {
  it("reaps a controlled editor descendant after its parent exits", async (t) => {
    const sandbox = provision("unit", "registered", 1);
    t.after(() => fs.rmSync(sandbox.root, { recursive: true, force: true }));
    const env: NodeJS.ProcessEnv = {
      ...environmentForTest(sandbox),
      AM_TUI_QA_EDITOR_SPAWN_CHILD: "1"
    };
    const run = await runBoundedProcess(sandbox.editor, [], {
      cwd: sandbox.repo,
      env,
      timeoutMs: 2_000,
      cleanupGraceMs: 300
    });
    assert.equal(run.exitCode, 0);
    const pidFile = env.AM_TUI_QA_DESCENDANT_PID_FILE!;
    const identity = await waitForJSONFile<RegisteredIdentity>(pidFile);
    t.after(() => {
      try {
        process.kill(identity.pid, "SIGKILL");
      } catch (error) {
        if ((error as NodeJS.ErrnoException).code !== "ESRCH") throw error;
      }
    });
    const cleanup = await cleanupRegisteredProcess(pidFile, 300);
    assert.equal(cleanup.complete, true, JSON.stringify(cleanup));
    assert.equal(processExists(identity.pid), false);
  });

  it("refuses a reused PID with a different process group", async (t) => {
    const directory = fs.mkdtempSync(path.join(process.env.TMPDIR ?? "/tmp", "tui-qa-identity-"));
    t.after(() => fs.rmSync(directory, { recursive: true, force: true }));
    const pidFile = path.join(directory, "descendant.json");
    fs.writeFileSync(pidFile, JSON.stringify({ pid: process.pid, pgid: 999_999 }) + "\n", { mode: 0o600 });
    const cleanup = await cleanupRegisteredProcess(pidFile, 50);
    assert.equal(cleanup.identityMatched, false);
    assert.equal(cleanup.complete, false);
    assert.deepEqual(cleanup.remaining, [process.pid]);
  });
});

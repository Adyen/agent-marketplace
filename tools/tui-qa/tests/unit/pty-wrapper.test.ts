import assert from "node:assert/strict";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import process from "node:process";
import { describe, it } from "node:test";
import { fileURLToPath } from "node:url";

import { runBoundedProcess } from "../../src/runner.js";

const moduleRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..", "..");
const wrapper = path.join(moduleRoot, "src", "pty-wrapper.js");
const run = (args: string[]) => runBoundedProcess(process.execPath, [wrapper, ...args], {
  cwd: os.tmpdir(),
  env: { PATH: process.env.PATH },
  timeoutMs: 2_000
});

describe("pty-wrapper options", () => {
  it("requires the target separator and an absolute existing cwd", async () => {
    assert.equal((await run(["/bin/true"])).exitCode, 127);
    assert.equal((await run(["--cwd", "relative", "--", "/bin/true"])).exitCode, 127);
    assert.equal((await run(["--cwd", "/does-not-exist", "--", "/bin/true"])).exitCode, 127);
  });

  it("launches the target in the selected cwd", async (t) => {
    const cwd = fs.mkdtempSync(path.join(os.tmpdir(), "tui-qa-cwd-"));
    t.after(() => fs.rmSync(cwd, { recursive: true, force: true }));
    const result = await run(["--cwd", cwd, "--", "/bin/pwd"]);
    const canonical = fs.realpathSync(cwd);
    assert.match(result.stdout, new RegExp(`^${canonical.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")}`, "m"));
    assert.equal(result.timedOut, false);
    assert.equal(result.cleanup.complete, true);
  });
});

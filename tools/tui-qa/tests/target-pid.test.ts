import assert from "node:assert/strict";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import process from "node:process";
import { test } from "node:test";

import { targetPID } from "./process-helpers.js";

test("targetPID waits for delayed PID publication", async (t) => {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), "agent-marketplace-target-pid-"));
  t.after(() => fs.rmSync(root, { recursive: true, force: true }));
  const fixture = { pidFile: path.join(root, "target.pid") };
  const publication = setTimeout(() => fs.writeFileSync(fixture.pidFile, `${process.pid}\n`), 25);

  try {
    assert.equal(await targetPID(fixture), process.pid);
  } finally {
    clearTimeout(publication);
  }
});

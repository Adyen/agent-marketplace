import assert from "node:assert/strict";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { test } from "node:test";

import { readRegularFile } from "./process-helpers.js";

test("QA reads one inspected regular descriptor and refuses symlinks", (t) => {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), "agent-marketplace-file-read-"));
  t.after(() => fs.rmSync(root, { recursive: true, force: true }));
  const file = path.join(root, "file");
  fs.writeFileSync(file, "exact bytes");
  assert.equal(readRegularFile(file).toString(), "exact bytes");
  const alias = path.join(root, "alias");
  fs.symlinkSync(file, alias);
  assert.throws(() => readRegularFile(alias), (error: NodeJS.ErrnoException) => error.code === "ELOOP");
  assert.throws(() => readRegularFile(root), /not a regular file/);
  assert.throws(() => readRegularFile(path.join(root, "absent")), (error: NodeJS.ErrnoException) => error.code === "ENOENT");
  assert.equal(fs.readFileSync(file, "utf8"), "exact bytes");
});

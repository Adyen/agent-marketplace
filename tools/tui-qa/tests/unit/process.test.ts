import assert from "node:assert/strict";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { describe, it } from "node:test";

import { parseProcessTable, processTreeMembers, writeProcessCleanupEvidence } from "../../src/process.js";

describe("parseProcessTable", () => {
  it("parses portable ps pid/ppid/pgid/state output", () => {
    assert.deepEqual(parseProcessTable(" 101  1 101 Ss+\n 102 101 101 S\n"), [
      { pid: 101, ppid: 1, pgid: 101, state: "Ss+" },
      { pid: 102, ppid: 101, pgid: 101, state: "S" }
    ]);
  });

  it("ignores malformed rows rather than inventing process identities", () => {
    assert.deepEqual(parseProcessTable("header\n0 nope\n"), []);
  });
});

describe("processTreeMembers", () => {
  it("tracks descendants after they enter a different process group", () => {
    const rows = parseProcessTable("100 1 100 S\n101 100 100 S\n102 101 102 S\n200 1 200 S\n");
    assert.deepEqual(processTreeMembers(100, rows).map((row) => row.pid), [100, 101, 102]);
  });
});

describe("writeProcessCleanupEvidence", () => {
  it("uses exclusive collision-safe filenames", (t) => {
    const directory = fs.mkdtempSync(path.join(os.tmpdir(), "tui-qa-evidence-"));
    t.after(() => fs.rmSync(directory, { recursive: true, force: true }));
    const evidence = {
      rootProcess: 100,
      processGroup: 100,
      detected: [100],
      terminated: [100],
      remaining: [],
      complete: true
    };
    const first = writeProcessCleanupEvidence(directory, evidence);
    const second = writeProcessCleanupEvidence(directory, evidence);
    assert.notEqual(first, second);
    assert.equal(fs.readFileSync(first, "utf8"), fs.readFileSync(second, "utf8"));
  });
});

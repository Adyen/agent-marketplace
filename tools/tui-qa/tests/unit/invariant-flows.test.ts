import assert from "node:assert/strict";
import { describe, it } from "node:test";

import { INVARIANT_FLOWS } from "../../src/invariant-flows.js";

describe("INVARIANT_FLOWS", () => {
  it("declares the complete required entrypoint, size, and exit matrix", () => {
    assert.deepEqual(INVARIANT_FLOWS.map((flow) => flow.id), [
      "bare-non-tty-help",
      "frame-baseline-80x24",
      "frame-constrained-50x12",
      "launch-bare-tty",
      "launch-explicit-tui",
      "no-tui-env",
      "no-tui-flag",
      "org-fallback",
      "quit-ctrl-c",
      "quit-q",
      "quit-sigterm",
      "tui-without-tty"
    ]);
    assert.equal(new Set(INVARIANT_FLOWS.map((flow) => flow.id)).size, INVARIANT_FLOWS.length);
    assert.equal(INVARIANT_FLOWS.every((flow) => flow.required), true);
  });

  it("uses only the approved 80x24 baseline and 50x12 minimum profiles", () => {
    const profiles = new Set(INVARIANT_FLOWS.filter((flow) => flow.driver === "pty").map((flow) => `${flow.columns}x${flow.rows}`));
    assert.deepEqual(profiles, new Set(["80x24", "50x12"]));
  });
});

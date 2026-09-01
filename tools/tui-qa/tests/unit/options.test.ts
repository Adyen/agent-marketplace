import assert from "node:assert/strict";
import { describe, it } from "node:test";

import { readBoundedPositiveInt } from "../../src/options.js";

describe("readBoundedPositiveInt", () => {
  it("uses defaults and accepts bounded integers", () => {
    assert.equal(readBoundedPositiveInt("ROWS", undefined, 24, 1_000), 24);
    assert.equal(readBoundedPositiveInt("ROWS", "50", 24, 1_000), 50);
  });

  it("rejects partial, zero, negative, and oversized values", () => {
    for (const value of ["12x", "0", "-1", "1001"]) {
      assert.throws(() => readBoundedPositiveInt("ROWS", value, 24, 1_000), /ROWS must/);
    }
  });
});

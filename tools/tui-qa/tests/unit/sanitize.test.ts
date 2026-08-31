import assert from "node:assert/strict";
import { describe, it } from "node:test";

import { escapeTerminal } from "../../src/sanitize.js";

const ESC = String.fromCharCode(27);

describe("escapeTerminal", () => {
  it("neutralizes CSI and OSC sequences", () => {
    const out = escapeTerminal(`safe ${ESC}[31mred ${ESC}[2J ${ESC}]0;owned${String.fromCharCode(7)}`);
    assert.equal(out.includes(ESC), false);
    assert.ok(out.includes("\\e[31m"));
    assert.ok(out.includes("\\e[2J"));
    assert.ok(out.includes("\\e]0;owned"));
    assert.equal(out.includes(String.fromCharCode(7)), false);
    assert.ok(out.includes("\\x07"));
    assert.equal(escapeTerminal(`${ESC}]0;title${ESC}\\`).includes(ESC), false);
  });

  it("escapes structural and bidi controls", () => {
    assert.equal(escapeTerminal("a\tb\nc\rd"), "a\\tb\\nc\\rd");
    assert.equal(escapeTerminal(`x${String.fromCharCode(1)}y`), "x\\x01y");
    assert.equal(escapeTerminal("\u009b\u009d"), "\\x9b\\x9d");
    assert.equal(escapeTerminal("a‮bc"), "a\\u202ebc");
  });

  it("preserves ordinary printable Unicode", () => {
    assert.equal(escapeTerminal("标题 e◌́ 缓存 😀"), "标题 e◌́ 缓存 😀");
  });
});

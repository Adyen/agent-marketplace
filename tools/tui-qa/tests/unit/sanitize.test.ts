import assert from "node:assert/strict";
import { describe, it } from "node:test";

import {
  allowedEnv,
  escapeTerminal,
  markdownCodeFence,
  markdownInline,
  redactCredentialShaped,
  redactURLCredentials,
  sanitizeMultiline,
  scanForSecrets,
  scanValueForSecrets
} from "../../src/sanitize.js";

const ESC = String.fromCharCode(27);
const bearerValue = `Bearer ${"abcdefghijklmnop"}`;
const githubToken = `ghp_${"abcdefghijklmnopqrst"}`;
const awsAccessKey = `AKIA${"ABCDEFGHIJKLMNOP"}`;
const privateKeyBlock = ["-----BEGIN ", "PRIVATE KEY-----\nprivate-body\n-----END ", "PRIVATE KEY-----"].join("");

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

describe("credential safety", () => {
  it("redacts URL, assignment, and bearer credential shapes", () => {
    assert.equal(redactURLCredentials("https://user:pass@example.invalid/path"), "https://***@example.invalid/path");
    const redacted = redactCredentialShaped(
      `token=super-secret Authorization: ${bearerValue} ${githubToken} ${awsAccessKey}\n${privateKeyBlock}`
    );
    for (const secret of ["super-secret", "abcdefghijklmnop", "ghp_", "AKIA", "private-body"]) {
      assert.equal(redacted.includes(secret), false, secret);
    }
    assert.deepEqual(scanValueForSecrets({ redacted }), []);
  });

  it("detects known secret shapes and sanitizes multiline output", () => {
    assert.deepEqual(scanForSecrets(awsAccessKey), ["aws-access-key"]);
    const sanitized = sanitizeMultiline(`safe\n${ESC}[31m token=super-secret`);
    assert.equal(sanitized.includes(ESC), false);
    assert.equal(sanitized.includes("super-secret"), false);
    assert.ok(sanitized.includes("\n"));
  });

  it("allowlists environment metadata without inheriting secrets", () => {
    assert.deepEqual(allowedEnv({ TERM: "xterm", GITHUB_TOKEN: "hidden", AM_TUI_QA_ROWS: "24" }), {
      AM_TUI_QA_ROWS: "24",
      TERM: "xterm"
    });
  });
});

describe("Markdown safety", () => {
  it("escapes inline and fence-breaking structure", () => {
    assert.equal(markdownInline("[x](https://evil) `code`"), "\\[x\\]\\(https://evil\\) \\`code\\`");
    const fenced = markdownCodeFence("before```after token=super-secret");
    assert.equal(fenced.includes("super-secret"), false);
    assert.ok(fenced.includes("\\`\\`\\`"));
    assert.ok(markdownCodeFence("one\ntwo").includes("one\ntwo"));
  });
});

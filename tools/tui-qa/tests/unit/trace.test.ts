import assert from "node:assert/strict";
import { describe, it } from "node:test";

import { TARGET_EXIT_BOUNDARY_VERSION, TERMINAL_RESET_SEQUENCE } from "../../src/protocol.js";
import { assessProductRestoration, splitTargetTrace } from "../../src/trace.js";

const ESC = String.fromCharCode(27);
const boundary = `\nAM_TUI_QA_TARGET_EXIT_BOUNDARY version=${TARGET_EXIT_BOUNDARY_VERSION} code=0 signal=none\n`;
const recovery = TERMINAL_RESET_SEQUENCE;

describe("splitTargetTrace", () => {
  it("keeps harness recovery outside the product segment", () => {
    const product = `${ESC}[?1049h${ESC}[?25l${ESC}[?1002h${ESC}[?1006h${ESC}[?2004h`;
    const segments = splitTargetTrace(product + boundary + recovery);

    assert.equal(segments.target, product);
    assert.equal(segments.harness, recovery);
    assert.equal(assessProductRestoration(segments.target, { canonical: false, echo: false }).complete, false);
    assert.equal(assessProductRestoration(product + recovery, { canonical: true, echo: true }).complete, true);
  });

  it("rejects unsupported versions before boundary-count errors", () => {
    const unsupported = boundary.replace(`version=${TARGET_EXIT_BOUNDARY_VERSION}`, "version=999");
    assert.throws(() => splitTargetTrace(unsupported + boundary), /unsupported target-exit boundary version 999/);
  });

  it("requires exactly one supported target-exit boundary", () => {
    assert.throws(() => splitTargetTrace("no boundary"), /exactly one/);
    assert.throws(() => splitTargetTrace(boundary + boundary), /exactly one/);
  });
});

describe("assessProductRestoration", () => {
  it("requires restoration after the final mode enable", () => {
    const wrongOrder = `${ESC}[?1049l${ESC}[?1049h${ESC}[?25h${ESC}[?25l`;
    const assessment = assessProductRestoration(wrongOrder, { canonical: true, echo: true });
    assert.equal(assessment.alternateScreen, false);
    assert.equal(assessment.cursorVisible, false);
    assert.equal(assessment.complete, false);
  });

  it("accepts ordered restoration and canonical echo state", () => {
    const clean =
      `${ESC}[?1049;1002;1003;1006;2004h${ESC}[?25l` +
      `${ESC}[?1002;1003;1006;2004l${ESC}[?25h${ESC}[?1049l`;
    assert.deepEqual(assessProductRestoration(clean, { canonical: true, echo: true }), {
      alternateScreen: true,
      cursorVisible: true,
      mouseDisabled: true,
      bracketedPasteDisabled: true,
      canonical: true,
      echo: true,
      complete: true
    });
    assert.equal(assessProductRestoration(clean, { canonical: null, echo: null }).complete, false);
  });
});

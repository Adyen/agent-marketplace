import assert from "node:assert/strict";
import { after, before } from "node:test";

import {
  clipboardWrites,
  defineFixture,
  findTextRun,
  locate,
  sgrDrag as drag,
  step,
  waitForExit,
  waitForInverse
} from "./helpers.js";

const journey = defineFixture("preview-selection", ["tui"], 80, 24, { overflowStatus: true });
before(journey.setup);
after(journey.cleanup);

// spec: tui-mouse-navigation/terminal-gestures-deliver-only-the-selected-text
// spec: tui-mouse-navigation/gestures-outside-selectable-text-do-not-copy
// spec: tui-mouse-navigation/scrolling-clears-selection-without-changing-navigation
journey.test("right-pane drag highlights, copies on release, and leaves wheel and clicks untouched", async (terminal) => {
  await step("settled overflowing catalog", () => {}, async () => {
    await locate(terminal, "beta-skill").expect();
    await locate(terminal, "~ in sync").unique().expect();
  });

  await step("wheel scroll still routes and emits no clipboard write", () => terminal.write("\x1b[<65;4;6M\x1b[<65;4;6M"), async () => {
    await locate(terminal, /Name:\s+skill-00/).unique().expect();
    assert.equal(await terminal.getClipboard(), "");
  });

  const copied = "Name:     skill-00";
  await step("drag across the detail pane highlights and copies on release", async () => {
    const run = await findTextRun(terminal, /Name:\s+skill-00/);
    await terminal.write(drag(run.x, run.y, run.x + run.width - 1, run.y));
  }, async () => {
    await waitForInverse(terminal, /Name:\s+skill-00/, 1);
    await terminal.waitClipboard(copied);
  });

  await step("wheel scroll clears the active highlight without a clipboard write", () => terminal.write("\x1b[<65;4;6M"), async () => {
    await locate(terminal, /Name:\s+skill-01/).unique().expect();
    await waitForInverse(terminal, /Name:\s+skill-01/, 0);
    assert.equal(await terminal.getClipboard(), copied);
  });

  await step("a click without drag copies nothing", () => terminal.write(drag(70, 5, 70, 5)), async () => {
    await waitForInverse(terminal, /Name:\s+skill-01/, 0);
    assert.equal(await terminal.getClipboard(), copied);
  });

  await step("a list-side drag selects nothing", () => terminal.write(drag(2, 3, 20, 3)), async () => {
    await waitForInverse(terminal, /Name:\s+skill-01/, 0);
    assert.equal(await terminal.getClipboard(), copied);
  });

  await step("quit without further input", () => terminal.write("q"), async () => {
    assert.equal((await waitForExit(terminal)).exitCode, 0);
  });
  assert.deepEqual(await clipboardWrites(terminal), [copied]);
});

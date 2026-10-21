import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { after, before } from "node:test";

import { clipboardWrites, defineFixture, findTextRun, locate, sgr, sgrDrag as drag, step, waitForExit, waitForInverse } from "./helpers.js";

// spec: tui-mouse-navigation/terminal-drag-transport-copies-selected-error-text
// spec: tui-mouse-navigation/error-screen-gestures-outside-displayed-text-do-not-copy
const journey = defineFixture("errors-selection", ["tui"]);
before(journey.setup);
after(journey.cleanup);

journey.test("errors-screen drags copy displayed error text and other gestures copy nothing", async (terminal) => {
  const fixture = journey.current();
  const repoConfig = path.join(fixture.repo, ".agents", "config.yaml");
  const source = (name: string): string => `  ${name}:\n    git: ${fixture.registryURL}\n    ref: main\n    path: skills\n`;
  const copied = "was selected but not found";
  const selected = /was selected but not found/;
  const innerCopied = "selected but not";
  const innerSelected = /selected but not/;

  await step("settled fixture baseline", () => {}, async () => {
    await locate(terminal, "~ in sync").unique().expect({ timeout: 30_000 });
  });
  fs.writeFileSync(
    repoConfig,
    `apiVersion: agent-marketplace/v4\nsources:\n${source("fixture")}${source("required-fixture")}resources:\n  fixture:\n    include:\n      - skill/beta-skill\n  required-fixture:\n    include:\n      - skill/gamma-skill\n    required: true\n`,
    { encoding: "utf8", mode: 0o600 }
  );
  await step("a missing required selection is an error", () => terminal.write("r"), async () => {
    await locate(terminal, /! 1 errors: skill "gamma/).unique().expect({ timeout: 30_000 });
  });
  await step("open the errors screen", () => terminal.write("e"), async () => {
    await locate(terminal, "Errors (1)").unique().expect();
    await locate(terminal, selected).unique().expect();
  });

  const run = await findTextRun(terminal, selected);
  const first = run.x;
  const last = run.x + run.width - 1;
  await step("dragging forward across error text highlights it before release", () => terminal.write(sgr(0, first, run.y, "M") + sgr(32, last, run.y, "M")), async () => {
    await waitForInverse(terminal, selected, 1);
    assert.equal(await terminal.getClipboard(), "", "the drag copied before release");
  });
  await step("releasing copies the highlighted text", () => terminal.write(sgr(0, last, run.y, "m")), async () => {
    await terminal.waitClipboard(copied);
  });
  await step("escape clears the selection and keeps the errors screen", () => terminal.write("\x1b"), async () => {
    await waitForInverse(terminal, selected, 0);
    await locate(terminal, "Errors (1)").unique().expect();
  });
  // The backward drag selects a shorter run, so its copy is a new value.
  const inner = await findTextRun(terminal, innerSelected);
  const innerFirst = inner.x;
  const innerLast = inner.x + inner.width - 1;
  await step("dragging backward highlights the shorter run before release", () => terminal.write(sgr(0, innerLast, inner.y, "M") + sgr(32, innerFirst, inner.y, "M")), async () => {
    await waitForInverse(terminal, innerSelected, 1);
    assert.equal(await terminal.getClipboard(), copied, "the backward drag copied before release");
  });
  await step("releasing copies the shorter run", () => terminal.write(sgr(0, innerFirst, inner.y, "m")), async () => {
    await terminal.waitClipboard(innerCopied);
  });
  await step("escape clears the backward selection", () => terminal.write("\x1b"), async () => {
    await waitForInverse(terminal, innerSelected, 0);
  });

  await step("a click without a drag copies nothing", async () => {
    const run = await findTextRun(terminal, selected);
    await terminal.write(sgr(0, run.x, run.y, "M") + sgr(0, run.x, run.y, "m"));
  }, async () => {
    await waitForInverse(terminal, selected, 0);
  });
  await step("a drag that starts in the footer copies nothing", async () => {
    const run = await findTextRun(terminal, /esc\/e close/);
    await terminal.write(drag(run.x, run.y, run.x + run.width - 1, run.y));
  }, async () => {
    await waitForInverse(terminal, /esc\/e close/, 0);
    await waitForInverse(terminal, selected, 0);
  });

  await step("e closes the errors screen", () => terminal.write("e"), async () => {
    await locate(terminal, "Errors (1)").expect({ not: true });
  });
  fs.writeFileSync(repoConfig, fixture.expectedRepoConfig, { encoding: "utf8", mode: 0o600 });
  await step("restoring the config clears the error", () => terminal.write("r"), async () => {
    await locate(terminal, "~ in sync").unique().expect({ timeout: 30_000 });
  });
  await step("quit", () => terminal.write("q"), async () => {
    assert.equal((await waitForExit(terminal)).exitCode, 0);
  });
  // Only the two drags wrote to the clipboard; the click and the footer drag
  // wrote nothing.
  assert.deepEqual(await clipboardWrites(terminal), [copied, innerCopied]);
});

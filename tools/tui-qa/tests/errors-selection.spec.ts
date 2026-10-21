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

  await step("a forward drag across error text highlights and copies it on release", async () => {
    const run = await findTextRun(terminal, selected);
    await terminal.write(drag(run.x, run.y, run.x + run.width - 1, run.y));
  }, async () => {
    await waitForInverse(terminal, selected, 1);
    await terminal.waitClipboard(copied);
  });
  await step("escape clears the selection and keeps the errors screen", () => terminal.write("\x1b"), async () => {
    await waitForInverse(terminal, selected, 0);
    await locate(terminal, "Errors (1)").unique().expect();
  });
  await step("a backward drag copies the same text", async () => {
    const run = await findTextRun(terminal, selected);
    await terminal.write(drag(run.x + run.width - 1, run.y, run.x, run.y));
  }, async () => {
    await waitForInverse(terminal, selected, 1);
    await terminal.waitClipboard(copied);
  });
  await step("escape clears the backward selection", () => terminal.write("\x1b"), async () => {
    await waitForInverse(terminal, selected, 0);
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
  assert.deepEqual(await clipboardWrites(terminal), [copied, copied]);
});

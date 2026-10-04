import assert from "node:assert/strict";

import { expect, test } from "@microsoft/tui-test";

import {
  declineLegacyMigration,
  defineFixture,
  findTextRun,
  osc52Capture,
  sgrDrag as drag,
  step,
  waitForExit,
  waitForInverse
} from "./helpers.js";

const journey = defineFixture("preview-selection", ["tui"], 80, 24, { overflowStatus: true });
test.use(journey.options);
test.beforeAll(journey.setup);
test.afterAll(journey.cleanup);

// spec: tui-mouse-navigation/terminal-gestures-deliver-only-the-selected-text
test("right-pane drag highlights, copies on release, and leaves wheel and clicks untouched", async ({ terminal }) => {
  const osc52 = osc52Capture(terminal);
  await declineLegacyMigration(terminal);

  await step("settled overflowing catalog", () => {}, async () => {
    await expect(terminal.getByText("beta-skill", { strict: false })).toBeVisible();
  });

  await step("wheel scroll still routes and emits no clipboard write", () => terminal.write("\x1b[<65;4;6M\x1b[<65;4;6M"), async () => {
    await expect(terminal.getByText(/Name:\s+skill-00/g)).toBeVisible();
    assert.equal(osc52.payloads().length, 0);
  });

  let dragSent = "";
  await step("drag across the detail pane highlights and copies on release", async () => {
    const run = findTextRun(terminal, /Name:\s+skill-00/g);
    assert.ok(run, "detail pane does not show the focused resource name");
    dragSent = drag(run.x, run.y, run.x + run.text.length - 1, run.y);
    await terminal.write(dragSent);
  }, async () => {
    try {
      await waitForInverse(terminal, /Name:\s+skill-00/g, 1);
    } catch (error) {
      const { shifts } = terminal.serialize();
      throw new Error(
        `${(error as Error).message}; drag=${JSON.stringify(dragSent)}; payloads=${JSON.stringify(osc52.payloads())}; ` +
          `shifts=${JSON.stringify([...shifts.entries()].filter(([, v]) => v.inverse !== undefined))}`
      );
    }
    const payloads = osc52.payloads();
    assert.equal(payloads.length, 1);
    assert.equal(Buffer.from(payloads[0], "base64").toString(), "Name:     skill-00");
  });

  await step("wheel scroll clears the active highlight without a clipboard write", () => terminal.write("\x1b[<65;4;6M"), async () => {
    await expect(terminal.getByText(/Name:\s+skill-01/g)).toBeVisible();
    await waitForInverse(terminal, /Name:\s+skill-01/g, 0);
    assert.equal(osc52.payloads().length, 1);
  });

  await step("a click without drag copies nothing", () => terminal.write(drag(70, 5, 70, 5)), async () => {
    await waitForInverse(terminal, /Name:\s+skill-01/g, 0);
    assert.equal(osc52.payloads().length, 1);
  });

  await step("a list-side drag selects nothing", () => terminal.write(drag(2, 3, 20, 3)), async () => {
    await waitForInverse(terminal, /Name:\s+skill-01/g, 0);
    assert.equal(osc52.payloads().length, 1);
  });

  await step("quit without further input", () => terminal.write("q"), async () => {
    assert.equal((await waitForExit(terminal)).exitCode, 0);
  });
});

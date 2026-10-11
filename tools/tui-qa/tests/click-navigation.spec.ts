import assert from "node:assert/strict";

import { expect, test } from "@microsoft/tui-test";

import {
  declineLegacyMigration,
  defineFixture,
  findTextRun,
  osc52Capture,
  sgr,
  sgrDrag as drag,
  step,
  waitForExit
} from "./helpers.js";

const journey = defineFixture("click-navigation", ["tui"], 80, 24, { overflowStatus: true });
test.use(journey.options);
test.beforeAll(journey.setup);
test.afterAll(journey.cleanup);

const click = (x: number, y: number): string => sgr(0, x, y, "M") + sgr(0, x, y, "m");

// spec: tui-mouse-navigation/terminal-clicks-navigate-the-running-tui
test("list clicks select and open resources while list drags stay inert", async ({ terminal }) => {
  const osc52 = osc52Capture(terminal);
  await declineLegacyMigration(terminal);

  await step("settled overflowing catalog", () => {}, async () => {
    await expect(terminal.getByText("beta-skill", { strict: false })).toBeVisible();
  });

  await step("wheel to a known selection", () => terminal.write("\x1b[<65;4;6M\x1b[<65;4;6M"), async () => {
    await expect(terminal.getByText(/Name:\s+skill-00/g)).toBeVisible();
  });

  const target = findTextRun(terminal, /skill-03\b/g);
  assert.ok(target, "resource list does not show skill-03");
  assert.ok(target.x < 32, `skill-03 was found outside the resource list at column ${target.x}`);

  await step("a drag starting on a list row changes nothing", () => terminal.write(drag(target.x, target.y, target.x + 8, target.y)), async () => {
    await expect(terminal.getByText(/Name:\s+skill-00/g)).toBeVisible();
    assert.equal(osc52.payloads().length, 0);
  });

  await step("a click selects the row under the pointer", () => terminal.write(click(target.x, target.y)), async () => {
    await expect(terminal.getByText(/Name:\s+skill-03/g)).toBeVisible();
    assert.equal(osc52.payloads().length, 0);
  });

  await step("a click on the selected row opens its detail view", () => terminal.write(click(target.x, target.y)), async () => {
    await expect(terminal.getByText("pgup/pgdn preview", { strict: false })).toBeVisible();
    await expect(terminal.getByText("skill-03/", { strict: false })).toBeVisible();
    assert.equal(osc52.payloads().length, 0);
  });

  await step("return to the list", () => terminal.write("\x1b"), async () => {
    await expect(terminal.getByText(/Name:\s+skill-03/g)).toBeVisible();
  });

  await step("quit without further input", () => terminal.write("q"), async () => {
    assert.equal((await waitForExit(terminal)).exitCode, 0);
  });
});

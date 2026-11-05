import assert from "node:assert/strict";
import { after, before } from "node:test";

import {
  clipboardWrites,
  defineFixture,
  findTextRun,
  locate,
  sgr,
  sgrDrag as drag,
  step,
  waitForExit
} from "./helpers.js";

const journey = defineFixture("click-navigation", ["tui"], 80, 24, { overflowStatus: true });
before(journey.setup);
after(journey.cleanup);

const click = (x: number, y: number): string => sgr(0, x, y, "M") + sgr(0, x, y, "m");

// spec: tui-mouse-navigation/terminal-clicks-navigate-the-running-tui
// spec: tui-mouse-navigation/clicking-a-resource-row-selects-it
// spec: tui-mouse-navigation/clicking-the-selected-resource-opens-its-details
journey.test("list clicks select and open resources while list drags stay inert", async (terminal) => {
  await step("settled overflowing catalog", () => {}, async () => {
    await locate(terminal, "beta-skill").expect();
    await locate(terminal, "~ in sync").unique().expect();
  });

  await step("wheel to a known selection", () => terminal.write("\x1b[<65;4;6M\x1b[<65;4;6M"), async () => {
    await locate(terminal, /Name:\s+skill-00/).unique().expect();
  });

  const target = await findTextRun(terminal, /skill-03\b/);
  assert.ok(target.x < 32, `skill-03 was found outside the resource list at column ${target.x}`);

  await step("a drag starting on a list row changes nothing", () => terminal.write(drag(target.x, target.y, target.x + 8, target.y)), async () => {
    await locate(terminal, /Name:\s+skill-00/).unique().expect();
    assert.equal(await terminal.getClipboard(), "");
  });

  await step("a click selects the row under the pointer", () => terminal.write(click(target.x, target.y)), async () => {
    await locate(terminal, /Name:\s+skill-03/).unique().expect();
    assert.equal(await terminal.getClipboard(), "");
  });

  await step("a click on the selected row opens its detail view", () => terminal.write(click(target.x, target.y)), async () => {
    await locate(terminal, "pgup/pgdn preview").expect();
    await locate(terminal, "skill-03/").expect();
    assert.equal(await terminal.getClipboard(), "");
  });

  await step("return to the list", () => terminal.write("\x1b"), async () => {
    await locate(terminal, /Name:\s+skill-03/).unique().expect();
  });

  await step("quit without further input", () => terminal.write("q"), async () => {
    assert.equal((await waitForExit(terminal)).exitCode, 0);
  });
  assert.deepEqual(await clipboardWrites(terminal), []);
});

import assert from "node:assert/strict";
import { after, before } from "node:test";

import { defineFixture, locate, step, waitForExit } from "./helpers.js";

const journey = defineFixture("ctrl-c-hard-exit", ["tui"], 50, 12);
before(journey.setup);
after(journey.cleanup);

journey.test("Ctrl+C exits cleanly from the minimum terminal", async (terminal) => {
  await step("ready minimum-size list", () => {}, async () => {
    await locate(terminal, "beta-skill").expect();
    await locate(terminal, "~ in sync").unique().expect();
    await locate(terminal, /: (commands|cmds) · \? help · q quit/).unique().expect();
  });

  await step("hard exit with Ctrl+C", () => terminal.write("\u0003"), async () => {
    assert.equal((await waitForExit(terminal)).exitCode, 0);
  });
});

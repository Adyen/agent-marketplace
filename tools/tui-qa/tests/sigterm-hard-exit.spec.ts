import assert from "node:assert/strict";
import process from "node:process";
import { after, before } from "node:test";

import { defineFixture, locate, step, waitForExit } from "./helpers.js";
import { targetPID } from "./process-helpers.js";

const journey = defineFixture("sigterm-hard-exit", ["tui"]);
before(journey.setup);
after(journey.cleanup);

journey.test("SIGTERM stops the published target process", async (terminal) => {
  const fixture = journey.current();

  await step("settled fixture baseline", () => {}, async () => {
    await locate(terminal, "beta-skill").expect();
    await locate(terminal, "alpha-skill").expect();
    await locate(terminal, "~ in sync").unique().expect();
  });

  const pid = await targetPID(fixture);
  await step("hard exit with SIGTERM", () => {
    process.kill(pid, "SIGTERM");
  }, async () => {
    await waitForExit(terminal);
    assert.throws(() => process.kill(pid, 0), (error: unknown) => (error as NodeJS.ErrnoException).code === "ESRCH");
  });
});

import assert from "node:assert/strict";
import process from "node:process";

import { expect, test } from "@microsoft/tui-test";

import { declineLegacyMigration, defineFixture, step, targetPID, waitForExit } from "./helpers.js";

const journey = defineFixture("sigterm-hard-exit", ["tui"]);
test.use(journey.options);
test.beforeAll(journey.setup);
test.afterAll(journey.cleanup);

test("SIGTERM stops the published target process", async ({ terminal }) => {
  const fixture = journey.current();
  await declineLegacyMigration(terminal);

  await step("settled fixture baseline", () => {}, async () => {
    await expect(terminal.getByText("beta-skill", { strict: false })).toBeVisible();
    await expect(terminal.getByText("alpha-skill", { strict: false })).toBeVisible();
    await expect(terminal.getByText("~ in sync")).toBeVisible();
  });

  const pid = await targetPID(fixture);
  await step("hard exit with SIGTERM", () => {
    process.kill(pid, "SIGTERM");
  }, async () => {
    await waitForExit(terminal);
    assert.throws(() => process.kill(pid, 0), (error: unknown) => (error as NodeJS.ErrnoException).code === "ESRCH");
  });
});

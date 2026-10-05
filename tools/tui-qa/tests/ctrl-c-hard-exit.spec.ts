import assert from "node:assert/strict";

import { expect, test } from "@microsoft/tui-test";

import { declineLegacyMigration, defineFixture, step, waitForExit } from "./helpers.js";

const journey = defineFixture("ctrl-c-hard-exit", ["tui"], 50, 12);
test.use(journey.options);
test.beforeAll(journey.setup);
test.afterAll(journey.cleanup);

test("Ctrl+C exits cleanly from the minimum terminal", async ({ terminal }) => {
  await declineLegacyMigration(terminal);
  await step("ready minimum-size list", () => {}, async () => {
    await expect(terminal.getByText("beta-skill", { strict: false })).toBeVisible();
    await expect(terminal.getByText("~ in sync")).toBeVisible();
    await expect(terminal.getByText(/: (commands|cmds) · \? help · q quit/g)).toBeVisible();
  });

  await step("hard exit with Ctrl+C", () => terminal.write("\u0003"), async () => {
    assert.equal((await waitForExit(terminal)).exitCode, 0);
  });
});

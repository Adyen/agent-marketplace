import assert from "node:assert/strict";

import { expect, test } from "@microsoft/tui-test";

import { declineLegacyMigration, defineFixture, step, waitForExit } from "./helpers.js";

const journey = defineFixture("search-and-select", ["tui"]);
test.use(journey.options);
test.beforeAll(journey.setup);
test.afterAll(journey.cleanup);

test("search keeps only alpha-skill selected", async ({ terminal }) => {
  await declineLegacyMigration(terminal);
  await step("settled fixture baseline", () => {}, async () => {
    await expect(terminal.getByText("beta-skill", { strict: false })).toBeVisible();
    await expect(terminal.getByText("alpha-skill", { strict: false })).toBeVisible();
    await expect(terminal.getByText("~ in sync")).toBeVisible();
  });

  await step("open search", () => terminal.write("/"), async () => {
    await expect(terminal.getByText(/\/ {20,}/g)).toBeVisible();
  });

  await step("filter for alpha", () => terminal.write("alpha"), async () => {
    await expect(terminal.getByText("/ alpha", { strict: false })).toBeVisible();
    await expect(terminal.getByText("alpha-skill", { strict: false })).toBeVisible();
    await expect(terminal.getByText(/Available\s+\(1\)/g)).toBeVisible();
    await expect(terminal.getByText("beta-skill", { strict: false })).not.toBeVisible();
  });

  await step("apply search", () => terminal.write("\r"), async () => {
    await expect(terminal.getByText("/ alpha", { strict: false })).not.toBeVisible();
    await expect(terminal.getByText(/Name:\s+alpha-skill/g)).toBeVisible();
    await expect(terminal.getByText("beta-skill", { strict: false })).not.toBeVisible();
  });

  await step("quit", () => terminal.write("q"), async () => {
    assert.equal((await waitForExit(terminal)).exitCode, 0);
  });
});

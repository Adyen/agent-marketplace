import assert from "node:assert/strict";

import { expect, test } from "@microsoft/tui-test";

import { defineFixture, step, waitForExit } from "./helpers.js";

const journey = defineFixture("help-and-return", ["tui"]);
test.use(journey.options);
test.beforeAll(journey.setup);
test.afterAll(journey.cleanup);

test("help closes back to the selected alpha-skill", async ({ terminal }) => {
  await step("settled fixture baseline", () => {}, async () => {
    await expect(terminal.getByText("beta-skill", { strict: false })).toBeVisible();
    await expect(terminal.getByText("alpha-skill", { strict: false })).toBeVisible();
    await expect(terminal.getByText("~ in sync")).toBeVisible();
  });

  await step("select available alpha-skill", () => terminal.write("G"), async () => {
    await expect(terminal.getByText(/Name:\s+alpha-skill/g)).toBeVisible();
    await expect(terminal.getByText(/Status:\s+available/g)).toBeVisible();
  });

  await step("open keyboard shortcuts", () => terminal.write("?"), async () => {
    await expect(terminal.getByText("agent-marketplace — Keyboard Shortcuts")).toBeVisible();
    await expect(terminal.getByText("jump to bottom")).toBeVisible();
    await expect(terminal.getByText("fuzzy search")).toBeVisible();
    await expect(terminal.getByText("clear search / close")).toBeVisible();
  });

  await step("close keyboard shortcuts", () => terminal.write("?"), async () => {
    await expect(terminal.getByText("agent-marketplace — Keyboard Shortcuts")).not.toBeVisible();
    await expect(terminal.getByText(/Name:\s+alpha-skill/g)).toBeVisible();
    await expect(terminal.getByText(/Status:\s+available/g)).toBeVisible();
  });

  await step("quit", () => terminal.write("q"), async () => {
    assert.equal((await waitForExit(terminal)).exitCode, 0);
  });
});

import assert from "node:assert/strict";

import { expect, test } from "@microsoft/tui-test";

import { defineFixture, step, waitForExit } from "./helpers.js";

const journey = defineFixture("resize-and-recover", ["tui"]);
test.use(journey.options);
test.beforeAll(journey.setup);
test.afterAll(journey.cleanup);

test("minimum-size gate preserves selection and blocks toggles", async ({ terminal }) => {
  await step("settled fixture baseline", () => {}, async () => {
    await expect(terminal.getByText("beta-skill", { strict: false })).toBeVisible();
    await expect(terminal.getByText("alpha-skill", { strict: false })).toBeVisible();
    await expect(terminal.getByText("~ in sync")).toBeVisible();
  });

  await step("select available alpha-skill", () => terminal.write("G"), async () => {
    await expect(terminal.getByText(/Name:\s+alpha-skill/g)).toBeVisible();
    await expect(terminal.getByText(/Status:\s+available/g)).toBeVisible();
  });

  await step("resize below minimum", () => terminal.resize(49, 11), async () => {
    await expect(terminal.getByText("Resize required")).toBeVisible();
    await expect(terminal.getByText("Minimum terminal: 50x12")).toBeVisible();
    await expect(terminal.getByText("q quit · Ctrl+C quit")).toBeVisible();
    await expect(terminal.getByText("alpha-skill", { strict: false })).not.toBeVisible();
    await expect(terminal.getByText("beta-skill", { strict: false })).not.toBeVisible();
  });

  await step("ignore toggle below minimum", async () => {
    terminal.write(" ");
    await new Promise((resolve) => setTimeout(resolve, 250));
  }, async () => {
    await expect(terminal.getByText("Resize required")).toBeVisible();
    await expect(terminal.getByText("Minimum terminal: 50x12")).toBeVisible();
    await expect(terminal.getByText("q quit · Ctrl+C quit")).toBeVisible();
    await expect(terminal.getByText("toggle local")).not.toBeVisible();
    await expect(terminal.getByText("alpha-skill", { strict: false })).not.toBeVisible();
    await expect(terminal.getByText("beta-skill", { strict: false })).not.toBeVisible();
  });

  await step("recover at exact minimum", () => terminal.resize(50, 12), async () => {
    await expect(terminal.getByText("Resize required")).not.toBeVisible();
    await expect(terminal.getByText(/Available\s+\(1\)/g)).toBeVisible();
    await expect(terminal.getByText(/Name:\s+alpha-skill/g)).toBeVisible();
    await expect(terminal.getByText("?:help · s:sync · q:quit")).toBeVisible();
  });

  await step("quit", () => terminal.write("q"), async () => {
    assert.equal((await waitForExit(terminal)).exitCode, 0);
  });
});

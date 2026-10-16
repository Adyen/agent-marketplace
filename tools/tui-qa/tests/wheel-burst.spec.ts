import assert from "node:assert/strict";

import { expect, test } from "@microsoft/tui-test";

import { defineFixture, step, waitForExit } from "./helpers.js";

const journey = defineFixture("wheel-burst", ["tui"], 80, 24, { overflowStatus: true });
test.use(journey.options);
test.beforeAll(journey.setup);
test.afterAll(journey.cleanup);

const wheelDown = (count: number): string => "\x1b[<65;4;6M".repeat(count);
const wheelUp = (count: number): string => "\x1b[<64;4;6M".repeat(count);

// spec: tui-mouse-navigation/terminal-wheel-burst-reaches-the-expected-resource
test("an encoded wheel burst with reversals reaches the expected resource without extra navigation keys", async ({ terminal }) => {
  const assertSelectedSkillDetails = async (name: string): Promise<void> => {
    await expect(terminal.getByText(new RegExp(`Name:\\s+${name}`, "g"))).toBeVisible();
    await expect(terminal.getByText(/Kind:\s+skill/g)).toBeVisible();
    await expect(terminal.getByText(new RegExp(`skill fixture/${name}`, "g"))).toBeVisible();
    await expect(terminal.getByText("Status overflow fixture", { strict: false })).toBeVisible();
  };

  await step("settled overflowing catalog", () => {}, async () => {
    await expect(terminal.getByText("beta-skill", { strict: false })).toBeVisible();
    await expect(terminal.getByText(/Enabled\s+\(31\)/g)).toBeVisible();
  });

  await step("wheel burst down then reversal", () => terminal.write(wheelDown(10) + wheelUp(3)), async () => assertSelectedSkillDetails("skill-05"));

  await step("first-boundary reversal burst", () => terminal.write(wheelUp(20) + wheelDown(4)), async () => assertSelectedSkillDetails("skill-02"));

  await step("quit without further input", () => terminal.write("q"), async () => {
    assert.equal((await waitForExit(terminal)).exitCode, 0);
  });
});

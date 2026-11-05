import assert from "node:assert/strict";
import { after, before } from "node:test";

import { defineFixture, locate, step, waitForExit } from "./helpers.js";

const journey = defineFixture("wheel-burst", ["tui"], 80, 24, { overflowStatus: true });
before(journey.setup);
after(journey.cleanup);

const wheelDown = (count: number): string => "\x1b[<65;4;6M".repeat(count);
const wheelUp = (count: number): string => "\x1b[<64;4;6M".repeat(count);

// spec: tui-mouse-navigation/terminal-wheel-burst-reaches-the-expected-resource
// spec: tui-mouse-navigation/list-selection-moves-by-one-entry
// spec: tui-mouse-navigation/rapid-wheel-reversals-preserve-selection
// spec: tui-mouse-navigation/reversing-at-the-list-boundary-is-immediate
journey.test("an encoded wheel burst with reversals reaches the expected resource without extra navigation keys", async (terminal) => {
  const assertSelectedSkillDetails = async (name: string): Promise<void> => {
    await locate(terminal, new RegExp(`Name:\\s+${name}`)).unique().expect();
    await locate(terminal, /Kind:\s+skill/).unique().expect();
    await locate(terminal, new RegExp(`skill fixture/${name}`)).unique().expect();
    await locate(terminal, "Status overflow fixture").expect();
  };

  await step("settled overflowing catalog", () => {}, async () => {
    await locate(terminal, "beta-skill").expect();
    await locate(terminal, /Enabled\s+\(31\)/).unique().expect();
    await locate(terminal, "~ in sync").unique().expect();
  });

  await step("wheel burst down then reversal", () => terminal.write(wheelDown(10) + wheelUp(3)), async () => assertSelectedSkillDetails("skill-05"));

  await step("first-boundary reversal burst", () => terminal.write(wheelUp(20) + wheelDown(4)), async () => assertSelectedSkillDetails("skill-02"));

  await step("quit without further input", () => terminal.write("q"), async () => {
    assert.equal((await waitForExit(terminal)).exitCode, 0);
  });
});

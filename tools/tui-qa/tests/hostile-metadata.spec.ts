import assert from "node:assert/strict";
import { after, before } from "node:test";

import { defineFixture, locate, step, waitForExit } from "./helpers.js";

// spec: tui-rendering/hostile-input-at-minimum-size
// spec: tui-rendering/malformed-rows-name-their-registry
const journey = defineFixture("hostile-metadata", ["tui"], 50, 12, { hostileMetadata: true });
before(journey.setup);
after(journey.cleanup);

journey.test("registry text with terminal control bytes renders as visible escapes at 50x12", async (terminal) => {
  // Narrow panes wrap long values, so compare without whitespace.
  const screenText = async (): Promise<string> => (await terminal.text()).replace(/\s+/g, "");
  const assertScreenHas = async (...expected: string[]): Promise<void> => {
    const screen = await screenText();
    for (const value of expected) {
      assert.ok(screen.includes(value), `the screen lost ${JSON.stringify(value)}:\n${await terminal.text()}`);
    }
  };

  await step("settled fixture baseline at the minimum size", () => {}, async () => {
    await locate(terminal, "~ in sync").unique().expect({ timeout: 30_000 });
    await locate(terminal, "q quit").unique().expect();
  });
  await step("the malformed skill's name and error render as escapes", () => terminal.write("G"), async () => {
    await locate(terminal, /Malformed\s+\(1\)/).unique().expect();
    await locate(terminal, /Name:\s+evil\\x1B\[2Jname/).unique().expect();
    // The label wraps inside the list pane, beside the detail pane, so match
    // its first row.
    await locate(terminal, "fixture/evil\\x1B[2Jn").unique().expect();
    await assertScreenHas("controlcharactersarenot");
    await locate(terminal, "(local)/evil").expect({ not: true });
    await locate(terminal, "q quit").unique().expect();
  });
  await step("select the Malformed header", () => terminal.write("k"), async () => {
    await locate(terminal, /Name:\s+evil/).expect({ not: true });
  });
  await step("select hostile-skill", () => terminal.write("k"), async () => {
    await locate(terminal, /Name:\s+hostile-skill/).unique().expect();
  });
  // The description sits below the detail pane's fold at 50x12.
  await step("its description shows the control bytes as escapes", () => terminal.resize(80, 24), async () => {
    // Only the 80-column footer has room for the toggle hint.
    await locate(terminal, "space toggle").unique().expect();
    await locate(terminal, "Hostile \\x1B]0;PWNED\\x07\\x1B[2J tail").unique().expect();
  });
  await step("back at the minimum size", () => terminal.resize(50, 12), async () => {
    await locate(terminal, /Name:\s+hostile-skill/).unique().expect();
    await locate(terminal, "q quit").unique().expect();
  });
  await step("its full view shows the body's control bytes as escapes", () => terminal.write("\r"), async () => {
    await locate(terminal, "esc/q back").unique().expect();
    await assertScreenHas("Hostile\\x1B[31mred\\x1B[0m");
  });
  await step("leaving the full view restores the list unchanged", () => terminal.write("\x1b"), async () => {
    await locate(terminal, "agent-marketplace — workspace").unique().expect();
    await locate(terminal, /Name:\s+hostile-skill/).unique().expect();
    await locate(terminal, "~ in sync").unique().expect();
  });
  await step("quit", () => terminal.write("q"), async () => {
    assert.equal((await waitForExit(terminal)).exitCode, 0);
  });
});

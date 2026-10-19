import assert from "node:assert/strict";
import { after, before } from "node:test";

import { defineFixture, locate, step, waitForExit } from "./helpers.js";

const journey = defineFixture("help-and-return", ["tui"]);
before(journey.setup);
after(journey.cleanup);

journey.test("help closes back to the selected alpha-skill", async (terminal) => {
  await step("settled fixture baseline", () => {}, async () => {
    await locate(terminal, "beta-skill").expect();
    await locate(terminal, "alpha-skill").expect();
    await locate(terminal, "~ in sync").unique().expect();
  });

  await step("select available alpha-skill", () => terminal.write("G"), async () => {
    await locate(terminal, /Name:\s+alpha-skill/).unique().expect();
    await locate(terminal, /Status:\s+available/).unique().expect();
  });

  await step("open keyboard shortcuts", () => terminal.write("?"), async () => {
    await locate(terminal, "agent-marketplace — Keyboard Shortcuts").unique().expect();
    await locate(terminal, "jump to bottom").unique().expect();
    await locate(terminal, "fuzzy search").unique().expect();
    await locate(terminal, "clear search / close").unique().expect();
  });

  await step("close keyboard shortcuts", () => terminal.write("?"), async () => {
    await locate(terminal, "agent-marketplace — Keyboard Shortcuts").expect({ not: true });
    await locate(terminal, /Name:\s+alpha-skill/).unique().expect();
    await locate(terminal, /Status:\s+available/).unique().expect();
  });

  await step("quit", () => terminal.write("q"), async () => {
    assert.equal((await waitForExit(terminal)).exitCode, 0);
  });
});

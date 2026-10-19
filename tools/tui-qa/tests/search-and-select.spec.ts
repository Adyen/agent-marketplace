import assert from "node:assert/strict";
import { after, before } from "node:test";

import { defineFixture, locate, step, waitForExit } from "./helpers.js";

const journey = defineFixture("search-and-select", ["tui"]);
before(journey.setup);
after(journey.cleanup);

journey.test("search keeps only alpha-skill selected", async (terminal) => {
  await step("settled fixture baseline", () => {}, async () => {
    await locate(terminal, "beta-skill").expect();
    await locate(terminal, "alpha-skill").expect();
    await locate(terminal, "~ in sync").unique().expect();
  });

  await step("open search", () => terminal.write("/"), async () => {
    await locate(terminal, /\/ {20,}/).unique().expect();
  });

  await step("filter for alpha", () => terminal.write("alpha"), async () => {
    await locate(terminal, "/ alpha").expect();
    await locate(terminal, "alpha-skill").expect();
  });

  await step("apply search keeps the filter visible", () => terminal.write("\r"), async () => {
    await locate(terminal, "/ alpha  1 match · esc clear · / edit").expect();
    await locate(terminal, "alpha-skill").expect();
    await locate(terminal, "beta-skill").expect({ not: true });
  });

  await step("open command palette", () => terminal.write("\x0b"), async () => {
    await locate(terminal, "Command palette").expect();
    await locate(terminal, "Clear search").expect();
    await locate(terminal, "enter run · esc close").expect();
  });

  await step("palette query is text not quit", () => terminal.write("q"), async () => {
    await locate(terminal, "Command palette").expect();
    await locate(terminal, "Quit").expect();
  });

  await step("esc closes palette", () => terminal.write("\x1b"), async () => {
    await locate(terminal, "Command palette").expect({ not: true });
    await locate(terminal, "alpha-skill").expect();
  });

  await step("select alpha-skill", () => terminal.write("G"), async () => {
    await locate(terminal, /Name:\s+alpha-skill/).unique().expect();
  });

  await step("esc in the list clears the filter", () => terminal.write("\x1b"), async () => {
    await locate(terminal, "esc clear").expect({ not: true });
    await locate(terminal, "beta-skill").expect();
  });

  await step("quit", () => terminal.write("q"), async () => {
    assert.equal((await waitForExit(terminal)).exitCode, 0);
  });
});

import assert from "node:assert/strict";
import { after, before } from "node:test";

import { defineFixture, locate, step, waitForExit } from "./helpers.js";

// spec: tui-rendering/malformed-rows-show-their-kind
const journey = defineFixture("malformed-kind", ["tui"], 80, 24, { directorySource: true, malformedCommand: true });
before(journey.setup);
after(journey.cleanup);

journey.test("a malformed command is labelled as a command, not a skill", async (terminal) => {
  await step("settled baseline lists the malformed command", () => {}, async () => {
    await locate(terminal, /Malformed\s+\(1\)/).unique().expect({ timeout: 30_000 });
    // The label wraps after its kind in the list pane.
    await locate(terminal, /✗\s+command/).unique().expect();
    await locate(terminal, "source/broken-command.md").unique().expect();
    await locate(terminal, /✗\s+skill/).expect({ not: true });
  });
  await step("select it", () => terminal.write("G"), async () => {
    await locate(terminal, /Kind:\s+command/).unique().expect();
    await locate(terminal, /Name:\s+broken-command\.md/).unique().expect();
  });
  await step("quit", () => terminal.write("q"), async () => {
    assert.equal((await waitForExit(terminal)).exitCode, 0);
  });
});

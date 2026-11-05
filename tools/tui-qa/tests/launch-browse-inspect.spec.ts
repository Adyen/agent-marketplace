import assert from "node:assert/strict";
import { after, before } from "node:test";

import { defineFixture, locate, step, waitForExit } from "./helpers.js";

// spec: tui-feedback/toggle-hints-follow-row-eligibility
const journey = defineFixture("launch-browse-inspect", []);
before(journey.setup);
after(journey.cleanup);

journey.test("bare launch browses the single status view and inspects alpha-skill", async (terminal) => {
  await step("settled fixture baseline", () => {}, async () => {
    await locate(terminal, "beta-skill").expect();
    await locate(terminal, "alpha-skill").expect();
    await locate(terminal, "~ in sync").unique().expect();
    await locate(terminal, /Enabled\s+\(1\)/).unique().expect();
    await locate(terminal, /Available\s+\(1\)/).unique().expect();
  });

  await step("former tab keys leave the list unchanged", () => terminal.write("123"), async () => {
    await locate(terminal, /Enabled\s+\(1\)/).unique().expect();
    await locate(terminal, /Available\s+\(1\)/).unique().expect();
    await locate(terminal, /1 Scope\s+2 Registry\s+3 Status/).expect({ not: true });
  });

  await step("search narrows the single list", () => terminal.write("/alpha\r"), async () => {
    // The detail can already name alpha in a stale, unfiltered frame.
    await locate(terminal, /\/ alpha\s+1 match\b/).unique().expect();
    await locate(terminal, "beta-skill").expect({ not: true });
    await locate(terminal, /Name:\s+alpha-skill/).unique().expect();
  });

  await step("clear the search", () => terminal.write("\u001b"), async () => {
    await locate(terminal, "/ alpha").expect({ not: true });
    await locate(terminal, "beta-skill").expect();
    await locate(terminal, /Enabled\s+\(1\)/).unique().expect();
    await locate(terminal, /Available\s+\(1\)/).unique().expect();
  });

  await step("fold and expand the Enabled section", () => terminal.write("gh"), async () => {
    await locate(terminal, /▶ Enabled\s+\(1\)/).unique().expect();
    await terminal.write("l");
    await locate(terminal, /▼ Enabled\s+\(1\)/).unique().expect();
  });

  await step("select available alpha-skill", () => terminal.write("G"), async () => {
    await locate(terminal, /Name:\s+alpha-skill/).unique().expect();
    await locate(terminal, /Status:\s+available/).unique().expect();
    // Space in the list toggles available rows too, so the hint offers it.
    await locate(terminal, "space = toggle · enter = full view").unique().expect();
  });

  await step("open full alpha-skill detail", () => terminal.write("\r"), async () => {
    await locate(terminal, "Alpha Skill").expect();
    await locate(terminal, "Alpha fixture body.").unique().expect();
  });

  await step("return to selected alpha-skill", () => terminal.write("\u001b"), async () => {
    await locate(terminal, /Enabled\s+\(1\)/).unique().expect();
    await locate(terminal, /1 Scope\s+2 Registry\s+3 Status/).expect({ not: true });
    await locate(terminal, /Name:\s+alpha-skill/).unique().expect();
  });

  await step("quit", () => terminal.write("q"), async () => {
    assert.equal((await waitForExit(terminal)).exitCode, 0);
  });
});

import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { after, before } from "node:test";

import { defineFixture, findTextRun, locate, step, waitForExit } from "./helpers.js";

// spec: tui-interactions/accept-and-discard-live-grouping-preview
// spec: tui-navigation/attention-rows-move-without-duplication
// spec: tui-navigation/grouping-changes-preserve-folds-and-resource-selection
// spec: tui-navigation/kind-and-source-searches-work-across-groupings
const journey = defineFixture("grouping-view-settings", ["tui"], 80, 24, { directorySource: true, malformedCommand: true });
before(journey.setup);
after(journey.cleanup);

journey.test("view settings preview, discard and accept groupings, which keep their own folds", async (terminal) => {
  const fixture = journey.current();
  const prefsPath = path.join(fixture.stateDir, "tui.json");
  const savedView = (): unknown => JSON.parse(fs.readFileSync(prefsPath, "utf8")).view;
  const eachResourceOnce = async (): Promise<void> => {
    for (const label of ["source/alpha-skill", "source/beta-skill", "source/broken-command.md"]) {
      await locate(terminal, label).unique().expect();
    }
  };

  await step("settled Status grouping", () => {}, async () => {
    await locate(terminal, /Available\s+\(2\)/).unique().expect({ timeout: 30_000 });
    await locate(terminal, /Malformed\s+\(1\)/).unique().expect();
    await locate(terminal, "~ in sync").unique().expect({ timeout: 30_000 });
  });
  await step("select alpha-skill", () => terminal.write("j"), async () => {
    await locate(terminal, /Name:\s+alpha-skill/).unique().expect();
  });

  // Discarding a preview, from the command palette entry point.
  await step("the palette filters to View settings", () => terminal.write(":view"), async () => {
    await locate(terminal, "▸ View settings (grouping)").unique().expect();
  });
  await step("enter opens View settings", () => terminal.write("\r"), async () => {
    await locate(terminal, /▸ Group by:\s+\[Status\]/).unique().expect();
  });
  await step("previewing Source pins the malformed row into Needs attention", () => terminal.write("l"), async () => {
    await locate(terminal, /Needs attention\s+\(1\)/).unique().expect();
    await locate(terminal, /source\s+\(2\)/).unique().expect();
    await locate(terminal, /Available\s+\(2\)/).expect({ not: true });
  });
  await step("esc restores the original view without writes", () => terminal.write("\x1b"), async () => {
    await locate(terminal, "View settings").expect({ not: true });
    await locate(terminal, /Available\s+\(2\)/).unique().expect();
    await locate(terminal, /Name:\s+alpha-skill/).unique().expect();
  });
  assert.equal(fs.existsSync(prefsPath), false, "discarding the preview wrote view preferences");

  // Accepting Source then Kind, from the v key.
  await step("v opens View settings", () => terminal.write("v"), async () => {
    await locate(terminal, "View settings").unique().expect();
  });
  await step("preview Source", () => terminal.write("l"), async () => {
    await locate(terminal, /Needs attention\s+\(1\)/).unique().expect();
  });
  await step("tab focuses the secondary axis", () => terminal.write("\t"), async () => {
    await locate(terminal, /▸ Then by:/).unique().expect();
  });
  await step("preview Kind as the secondary axis", () => terminal.write("l"), async () => {
    await locate(terminal, /Skills\s+\(2\)/).unique().expect();
  });
  await step("enter keeps Source then Kind, and the selection follows alpha-skill", () => terminal.write("\r"), async () => {
    await locate(terminal, "View settings").expect({ not: true });
    await locate(terminal, /Name:\s+alpha-skill/).unique().expect();
    await eachResourceOnce();
  });
  assert.deepEqual(savedView(), { groupBy: "source", thenBy: "kind" });
  const attention = await findTextRun(terminal, /Needs attention\s+\(1\)/);
  const source = await findTextRun(terminal, /source\s+\(2\)/);
  const malformed = await findTextRun(terminal, /broken-command\.md/);
  assert.ok(attention.y < malformed.y && malformed.y < source.y, "the attention section with the malformed row must come first");

  await step("h folds Skills and selects its header", () => terminal.write("h"), async () => {
    await locate(terminal, /▶ Skills\s+\(2\)/).unique().expect();
    await locate(terminal, "source/alpha-skill").expect({ not: true });
    await locate(terminal, "Select a resource to see details").unique().expect();
  });
  await step("open View settings to switch to Kind", () => terminal.write("v"), async () => {
    await locate(terminal, /▸ Group by:\s+Status\s+\[Source\]/).unique().expect();
  });
  await step("Kind grouping previews its own unfolded Skills section", () => terminal.write("l"), async () => {
    await locate(terminal, /▸ Group by:.*\[Kind\]/).unique().expect();
    await locate(terminal, /▼ Skills\s+\(2\)/).unique().expect();
    await locate(terminal, /source\s+\(2\)/).expect({ not: true });
  });
  await step("accept Kind", () => terminal.write("\r"), async () => {
    await locate(terminal, "View settings").expect({ not: true });
    await eachResourceOnce();
  });
  assert.deepEqual(savedView(), { groupBy: "kind", thenBy: "none" });
  await step("open View settings to return to Source", () => terminal.write("v"), async () => {
    await locate(terminal, /▸ Group by:.*\[Kind\]/).unique().expect();
  });
  await step("preview Source", () => terminal.write("h"), async () => {
    await locate(terminal, /▸ Group by:\s+Status\s+\[Source\]/).unique().expect();
    await locate(terminal, /source\s+\(2\)/).unique().expect();
  });
  await step("focus the secondary axis again", () => terminal.write("\t"), async () => {
    await locate(terminal, /▸ Then by:\s+\[None\]/).unique().expect();
  });
  await step("previewing Kind under Source restores its fold", () => terminal.write("l"), async () => {
    await locate(terminal, /▶ Skills\s+\(2\)/).unique().expect();
  });
  await step("accept Source then Kind", () => terminal.write("\r"), async () => {
    await locate(terminal, "View settings").expect({ not: true });
    await locate(terminal, /▶ Skills\s+\(2\)/).unique().expect();
    await locate(terminal, "source/alpha-skill").expect({ not: true });
  });
  assert.deepEqual(savedView(), { groupBy: "source", thenBy: "kind" });

  await step("type a source-label query", () => terminal.write("/source"), async () => {
    await locate(terminal, "enter apply · esc cancel").unique().expect();
    await locate(terminal, "/ source").unique().expect();
  });
  await step("applying it reaches every resource from that source", () => terminal.write("\r"), async () => {
    await locate(terminal, "/ source  3 matches").unique().expect();
    await locate(terminal, "source/alpha-skill").unique().expect();
  });
  await step("clearing the search keeps the saved fold", () => terminal.write("\x1b"), async () => {
    await locate(terminal, "/ source").expect({ not: true });
    await locate(terminal, /▶ Skills\s+\(2\)/).unique().expect();
  });
  await step("type a query with no matches", () => terminal.write("/zzzz"), async () => {
    await locate(terminal, "/ zzzz").unique().expect();
  });
  await step("applying it leaves an empty list", () => terminal.write("\r"), async () => {
    await locate(terminal, "/ zzzz  0 matches").unique().expect();
    await locate(terminal, "No resources found").unique().expect();
  });
  await step("clearing it restores the grouping", () => terminal.write("\x1b"), async () => {
    await locate(terminal, /Needs attention\s+\(1\)/).unique().expect();
    await locate(terminal, /▶ Skills\s+\(2\)/).unique().expect();
  });

  await step("quit", () => terminal.write("q"), async () => {
    assert.equal((await waitForExit(terminal)).exitCode, 0);
  });
});

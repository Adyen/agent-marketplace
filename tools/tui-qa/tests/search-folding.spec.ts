import assert from "node:assert/strict";
import { after, before } from "node:test";

import { defineFixture, locate, step, waitForExit } from "./helpers.js";

// spec: tui-navigation/fold-a-category-in-applied-search
// spec: tui-navigation/change-query-reveals-previously-folded-matches
const journey = defineFixture("search-folding", ["tui"]);
before(journey.setup);
after(journey.cleanup);

journey.test("fold keys work in an applied search without touching the query or the browsing folds", async (terminal) => {
  const folded = (section: string): RegExp => new RegExp(`▶ ${section}\\s+\\(1\\)`);
  const expanded = (section: string): RegExp => new RegExp(`▼ ${section}\\s+\\(1\\)`);

  await step("settled fixture baseline", () => {}, async () => {
    await locate(terminal, "~ in sync").unique().expect({ timeout: 30_000 });
  });
  await step("select alpha-skill", () => terminal.write("G"), async () => {
    await locate(terminal, /Name:\s+alpha-skill/).unique().expect();
  });
  await step("select the Available header", () => terminal.write("k"), async () => {
    await locate(terminal, "Select a resource to see details").unique().expect();
  });
  await step("za folds Available while browsing", () => terminal.write("za"), async () => {
    await locate(terminal, folded("Available")).unique().expect();
    await locate(terminal, "fixture/alpha-skill").expect({ not: true });
  });

  await step("a new query reveals the match in the folded section", () => terminal.write("/alpha\r"), async () => {
    await locate(terminal, "/ alpha  1 match").unique().expect();
    await locate(terminal, expanded("Available")).unique().expect();
    await locate(terminal, /Name:\s+alpha-skill/).unique().expect();
    await locate(terminal, "fixture/alpha-skill").unique().expect();
  });
  await step("select the matching category", () => terminal.write("k"), async () => {
    await locate(terminal, "Select a resource to see details").unique().expect();
  });
  await step("za folds it inside the search", () => terminal.write("za"), async () => {
    await locate(terminal, folded("Available")).unique().expect();
    await locate(terminal, "/ alpha  1 match").unique().expect();
    await locate(terminal, "fixture/alpha-skill").expect({ not: true });
  });
  await step("reopening the unchanged query keeps the search fold", () => terminal.write("/\r"), async () => {
    await locate(terminal, "/ alpha  1 match").unique().expect();
    await locate(terminal, folded("Available")).unique().expect();
    await locate(terminal, "fixture/alpha-skill").expect({ not: true });
  });
  await step("zR expands every category", () => terminal.write("zR"), async () => {
    await locate(terminal, expanded("Available")).unique().expect();
    await locate(terminal, "fixture/alpha-skill").unique().expect();
  });
  await step("zM folds every category", () => terminal.write("zM"), async () => {
    await locate(terminal, folded("Available")).unique().expect();
    await locate(terminal, "/ alpha  1 match").unique().expect();
    await locate(terminal, "fixture/alpha-skill").expect({ not: true });
  });
  await step("enter on the header expands it and keeps it selected", () => terminal.write("\r"), async () => {
    await locate(terminal, expanded("Available")).unique().expect();
    await locate(terminal, "Select a resource to see details").unique().expect();
  });
  await step("fold again before changing the query", () => terminal.write("za"), async () => {
    await locate(terminal, folded("Available")).unique().expect();
  });
  await step("a changed query reveals the matches again", () => terminal.write("/\x7f\r"), async () => {
    await locate(terminal, "/ alph  1 match").unique().expect();
    await locate(terminal, expanded("Available")).unique().expect();
    await locate(terminal, /Name:\s+alpha-skill/).unique().expect();
    await locate(terminal, "fixture/alpha-skill").unique().expect();
  });

  await step("clearing the query restores the browsing folds", () => terminal.write("\x1b"), async () => {
    await locate(terminal, /\/ alph/).expect({ not: true });
    await locate(terminal, expanded("Enabled")).unique().expect();
    await locate(terminal, folded("Available")).unique().expect();
  });
  await step("zR expands every browsing category", () => terminal.write("zR"), async () => {
    await locate(terminal, expanded("Available")).unique().expect();
    await locate(terminal, "fixture/alpha-skill").unique().expect();
  });
  await step("zM folds every browsing category", () => terminal.write("zM"), async () => {
    await locate(terminal, folded("Enabled")).unique().expect();
    await locate(terminal, folded("Available")).unique().expect();
  });
  await step("quit", () => terminal.write("q"), async () => {
    assert.equal((await waitForExit(terminal)).exitCode, 0);
  });
});

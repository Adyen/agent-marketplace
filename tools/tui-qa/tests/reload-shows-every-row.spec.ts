import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { after, before } from "node:test";

import { defineFixture, locate, step, waitForExit } from "./helpers.js";

// spec: tui-navigation/reload-with-room-for-every-row-shows-the-whole-list
const journey = defineFixture("reload-shows-every-row", ["tui"]);
before(journey.setup);
after(journey.cleanup);

journey.test("a reload that inserts rows above the selected header keeps every row of a fitting list visible", async (terminal) => {
  const fixture = journey.current();
  const repoConfig = path.join(fixture.repo, ".agents", "config.yaml");

  await step("settled fixture baseline", () => {}, async () => {
    await locate(terminal, /Enabled\s+\(1\)/).unique().expect();
    await locate(terminal, /Available\s+\(1\)/).unique().expect();
    await locate(terminal, "~ in sync").unique().expect({ timeout: 30_000 });
  });
  await step("select beta-skill", () => terminal.write("j"), async () => {
    await locate(terminal, /Name:\s+beta-skill/).unique().expect();
  });
  await step("select the Available header below it", () => terminal.write("j"), async () => {
    await locate(terminal, "Select a resource to see details").unique().expect();
  });

  // A selection the registry does not provide adds a Broken section between
  // Enabled and the selected Available header.
  fs.writeFileSync(repoConfig, fixture.expectedRepoConfig.replace("      - skill/beta-skill\n", "      - skill/beta-skill\n      - skill/gamma-skill\n"), {
    encoding: "utf8",
    mode: 0o600
  });
  await step("reload keeps the top sections visible and the header selected", () => terminal.write("r"), async () => {
    await locate(terminal, /Broken\s+\(1\)/).unique().expect({ timeout: 30_000 });
    await locate(terminal, /Enabled\s+\(1\)/).unique().expect();
    await locate(terminal, "fixture/beta-skill").unique().expect();
    await locate(terminal, /Available\s+\(1\)/).unique().expect();
    await locate(terminal, "Select a resource to see details").unique().expect();
  });
  await step("the selection is still the Available header", () => terminal.write("j"), async () => {
    await locate(terminal, /Name:\s+alpha-skill/).unique().expect();
  });

  fs.writeFileSync(repoConfig, fixture.expectedRepoConfig, { encoding: "utf8", mode: 0o600 });
  await step("restore the selection", () => terminal.write("r"), async () => {
    await locate(terminal, /Broken\s+\(1\)/).expect({ not: true });
    await locate(terminal, "~ in sync").unique().expect({ timeout: 30_000 });
  });
  await step("quit", () => terminal.write("q"), async () => {
    assert.equal((await waitForExit(terminal)).exitCode, 0);
  });
});

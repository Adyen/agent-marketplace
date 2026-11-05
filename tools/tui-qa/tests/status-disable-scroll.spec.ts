import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { after, before } from "node:test";

import { defineFixture, locate, step, waitForExit } from "./helpers.js";

const journey = defineFixture("status-disable-scroll", ["tui"], 80, 24, { overflowStatus: true });
before(journey.setup);
after(journey.cleanup);

journey.test("disabling an overflowing Status row keeps its section visible and selects the next resource", async (terminal) => {
  const fixture = journey.current();

  await step("settled overflowing catalog", () => {}, async () => {
    await locate(terminal, "beta-skill").expect();
    await locate(terminal, "~ in sync").unique().expect();
  });
  await step("status buckets are the single view", () => {}, async () => {
    await locate(terminal, /Enabled\s+\(31\)/).unique().expect();
  });
  await step("select the first enabled skill", () => terminal.write("j"), async () => {
    await locate(terminal, /Name:\s+beta-skill/).unique().expect();
  });
  await step("select the second enabled skill", () => terminal.write("j"), async () => {
    await locate(terminal, /Name:\s+skill-00/).unique().expect();
  });
  await step("open project toggle", () => terminal.write(" "), async () => {
    await locate(terminal, '"skill-00"').expect();
    await locate(terminal, "toggle project").unique().expect();
  });
  await step("disable without scrolling away from the section header", () => terminal.write("p"), async () => {
    await locate(terminal, /Name:\s+skill-01/).unique().expect();
    await locate(terminal, /Enabled\s+\(30\)/).unique().expect();
  });
  const config = fs.readFileSync(path.join(fixture.repo, ".agents", "config.yaml"), "utf8");
  assert.doesNotMatch(config.split("    exclude:")[0], /      - skill\/skill-00\n/);
  assert.match(config, /    exclude:\n      - skill\/skill-00\n/);
  await step("offer sync before quitting with pending drift", () => terminal.write("q"), async () => {
    await locate(terminal, "pending changes not synced").expect();
  });
  await step("quit without syncing", () => terminal.write("n"), async () => {
    assert.equal((await waitForExit(terminal)).exitCode, 0);
  });
});

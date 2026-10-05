import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";

import { expect, test } from "@microsoft/tui-test";

import { declineLegacyMigration, defineFixture, step, waitForExit } from "./helpers.js";

const journey = defineFixture("status-disable-scroll", ["tui"], 80, 24, { overflowStatus: true });
test.use(journey.options);
test.beforeAll(journey.setup);
test.afterAll(journey.cleanup);

test("disabling an overflowing Status row keeps its section visible and selects the next resource", async ({ terminal }) => {
  const fixture = journey.current();
  await declineLegacyMigration(terminal);

  await step("settled overflowing catalog", () => {}, async () => {
    await expect(terminal.getByText("beta-skill", { strict: false })).toBeVisible();
    await expect(terminal.getByText("~ in sync")).toBeVisible();
  });
  await step("status buckets are the single view", () => {}, async () => {
    await expect(terminal.getByText(/Enabled\s+\(31\)/g)).toBeVisible();
  });
  await step("select the first enabled skill", () => terminal.write("j"), async () => {
    await expect(terminal.getByText(/Name:\s+beta-skill/g)).toBeVisible();
  });
  await step("select the second enabled skill", () => terminal.write("j"), async () => {
    await expect(terminal.getByText(/Name:\s+skill-00/g)).toBeVisible();
  });
  await step("open project toggle", () => terminal.write(" "), async () => {
    await expect(terminal.getByText('"skill-00"', { strict: false })).toBeVisible();
    await expect(terminal.getByText("toggle project")).toBeVisible();
  });
  await step("disable without scrolling away from the section header", () => terminal.write("p"), async () => {
    await expect(terminal.getByText(/Name:\s+skill-01/g)).toBeVisible();
    await expect(terminal.getByText(/Enabled\s+\(30\)/g)).toBeVisible();
  });
  const config = fs.readFileSync(path.join(fixture.repo, ".agents", "config.yaml"), "utf8");
  assert.doesNotMatch(config.split("      exclude:")[0], /        - skill-00\n/);
  assert.match(config, /      exclude:\n        - skill-00\n/);
  await step("offer sync before quitting with pending drift", () => terminal.write("q"), async () => {
    await expect(terminal.getByText("pending changes not synced", { strict: false })).toBeVisible();
  });
  await step("quit without syncing", () => terminal.write("n"), async () => {
    assert.equal((await waitForExit(terminal)).exitCode, 0);
  });
});

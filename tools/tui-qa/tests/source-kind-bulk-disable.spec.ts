import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";

import { expect, test } from "@microsoft/tui-test";

import { defineFixture, step, waitForExit } from "./helpers.js";

// spec: tui-interactions/filtered-folded-descendants-and-skipped-rows
// spec: tui-startup/confirmed-preferences-survive-restart-without-config-writes
const journey = defineFixture("source-kind-bulk-disable", ["tui"]);
test.use(journey.options);
test.beforeAll(journey.setup);
test.afterAll(journey.cleanup);

test("Source → Kind grouping bulk-disables a folded section at project scope and persists", async ({ terminal }) => {
  const fixture = journey.current();

  await step("settled fixture baseline", () => {}, async () => {
    await expect(terminal.getByText("beta-skill", { strict: false })).toBeVisible();
    await expect(terminal.getByText("~ in sync")).toBeVisible();
  });

  await step("open View settings", () => terminal.write("v"), async () => {
    await expect(terminal.getByText("View settings")).toBeVisible();
    await expect(terminal.getByText("Group by:", { strict: false })).toBeVisible();
  });
  await step("preview Source as the primary axis", () => terminal.write("l"), async () => {
    await expect(terminal.getByText("fixture / skills", { strict: false })).toBeVisible();
  });
  await step("focus the secondary axis", () => terminal.write("\t"), async () => {
    // The focus indicator must move off the primary field onto "Then by".
    await expect(terminal.getByText(/▸ Then by:/g)).toBeVisible();
    await expect(terminal.getByText(/▸ Group by:/g)).not.toBeVisible();
  });
  await step("preview Kind as the secondary axis", () => terminal.write("l"), async () => {
    await expect(terminal.getByText("Skills", { strict: false })).toBeVisible();
  });
  await step("accept and persist the grouping", () => terminal.write("\r"), async () => {
    await expect(terminal.getByText("View settings")).not.toBeVisible();
    await expect(terminal.getByText(/fixture \/ skills\s+\(2\)/g)).toBeVisible();
    const prefsPath = path.join(fixture.stateDir, "tui.json");
    assert.ok(fs.existsSync(prefsPath), "view preferences file was not written");
    const prefs = JSON.parse(fs.readFileSync(prefsPath, "utf8"));
    assert.deepEqual(prefs.view, { groupBy: "source", thenBy: "kind" }, "persisted axes");
  });

  await step("fold the source section", () => terminal.write("h"), async () => {
    await expect(terminal.getByText("beta-skill", { strict: false })).not.toBeVisible();
    await expect(terminal.getByText("alpha-skill", { strict: false })).not.toBeVisible();
    await expect(terminal.getByText(/fixture \/ skills\s+\(2\)/g)).toBeVisible();
  });

  await step("open the section bulk modal", () => terminal.write(" "), async () => {
    await expect(terminal.getByText("Toggle all in fixture / skills", { strict: false })).toBeVisible();
    await expect(terminal.getByText(/2 matching · 2 eligible · 0 skipped/g)).toBeVisible();
  });
  await step("choose project scope", () => terminal.write("p"), async () => {
    await expect(terminal.getByText(/scope: project/g)).toBeVisible();
    await expect(terminal.getByText(/enable.*disable/g)).toBeVisible();
  });
  await step("choose disable", () => terminal.write("-"), async () => {
    await expect(terminal.getByText(/disable 1 resources in the project layer \(1 skipped\)/g)).toBeVisible();
  });
  await step("confirm the batch", () => terminal.write("\r"), async () => {
    await expect(terminal.getByText("Disabled 1 resources", { strict: false })).toBeVisible();
  });
  const config = fs.readFileSync(path.join(fixture.repo, ".agents", "config.yaml"), "utf8");
  assert.match(config, /    exclude:\n      - skill\/beta-skill\n/);
  await step("wait for authoritative pending drift", () => {}, async () => {
    await expect(terminal.getByText("~ 1 out of sync")).toBeVisible();
  });

  await step("offer sync before quitting with pending drift", () => terminal.write("q"), async () => {
    await expect(terminal.getByText("pending changes not synced", { strict: false })).toBeVisible();
  });
  await step("quit without syncing", () => terminal.write("n"), async () => {
    assert.equal((await waitForExit(terminal)).exitCode, 0);
  });
});

test("the accepted Source → Kind grouping survives restart", async ({ terminal }) => {
  await step("restored grouping without status buckets", () => {}, async () => {
    // The persisted Source → Kind grouping is restored: the source header
    // carries its kind sub-section and each resource renders exactly once.
    await expect(terminal.getByText(/fixture \/ skills/g)).toBeVisible();
    await expect(terminal.getByText(/Skills\s+\(\d+\)/g)).toBeVisible();
    await expect(terminal.getByText("alpha-skill", { strict: false })).toBeVisible();
    await expect(terminal.getByText("beta-skill", { strict: false })).toBeVisible();
    await expect(terminal.getByText(/Enabled\s+\(/g)).not.toBeVisible();
  });
  await step("offer sync before quitting with pending drift", () => terminal.write("q"), async () => {
    await expect(terminal.getByText("pending changes not synced", { strict: false })).toBeVisible();
  });
  await step("quit without syncing", () => terminal.write("n"), async () => {
    assert.equal((await waitForExit(terminal)).exitCode, 0);
  });
});

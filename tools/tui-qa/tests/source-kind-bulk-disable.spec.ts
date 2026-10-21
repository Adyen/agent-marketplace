import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { after, before } from "node:test";

import { defineFixture, locate, step, waitForExit } from "./helpers.js";

// spec: tui-interactions/filtered-folded-descendants-and-skipped-rows
// spec: tui-startup/confirmed-preferences-survive-restart-without-config-writes
const journey = defineFixture("source-kind-bulk-disable", ["tui"]);
before(journey.setup);
after(journey.cleanup);

journey.test("Source → Kind grouping bulk-disables a folded section at project scope and persists", async (terminal) => {
  const fixture = journey.current();

  await step("settled fixture baseline", () => {}, async () => {
    await locate(terminal, "beta-skill").expect();
    await locate(terminal, "~ in sync").unique().expect();
  });

  await step("open View settings", () => terminal.write("v"), async () => {
    await locate(terminal, "View settings").unique().expect();
    await locate(terminal, "Group by:").expect();
  });
  await step("preview Source as the primary axis", () => terminal.write("l"), async () => {
    await locate(terminal, "fixture / skills").expect();
  });
  await step("focus the secondary axis", () => terminal.write("\t"), async () => {
    // The focus indicator must move off the primary field onto "Then by".
    await locate(terminal, /▸ Then by:/).unique().expect();
    await locate(terminal, /▸ Group by:/).expect({ not: true });
  });
  await step("preview Kind as the secondary axis", () => terminal.write("l"), async () => {
    await locate(terminal, "Skills").expect();
  });
  await step("accept and persist the grouping", () => terminal.write("\r"), async () => {
    await locate(terminal, "View settings").expect({ not: true });
    await locate(terminal, /fixture \/ skills\s+\(2\)/).unique().expect();
    const prefsPath = path.join(fixture.stateDir, "tui.json");
    assert.ok(fs.existsSync(prefsPath), "view preferences file was not written");
    const prefs = JSON.parse(fs.readFileSync(prefsPath, "utf8"));
    assert.deepEqual(prefs.view, { groupBy: "source", thenBy: "kind" }, "persisted axes");
  });

  await step("fold the source section", () => terminal.write("h"), async () => {
    await locate(terminal, "beta-skill").expect({ not: true });
    await locate(terminal, "alpha-skill").expect({ not: true });
    await locate(terminal, /fixture \/ skills\s+\(2\)/).unique().expect();
  });

  await step("open the section bulk modal", () => terminal.write(" "), async () => {
    await locate(terminal, "Toggle all in fixture / skills").expect();
    await locate(terminal, /2 matching · 2 eligible · 0 skipped/).unique().expect();
  });
  await step("choose project scope", () => terminal.write("p"), async () => {
    await locate(terminal, /scope: project/).unique().expect();
    await locate(terminal, /enable.*disable/).unique().expect();
  });
  await step("choose disable", () => terminal.write("-"), async () => {
    await locate(terminal, /disable 1 resources in the project layer \(1 skipped\)/).unique().expect();
  });
  await step("confirm the batch", () => terminal.write("\r"), async () => {
    await locate(terminal, "Disabled 1 resources").expect();
  });
  const config = fs.readFileSync(path.join(fixture.repo, ".agents", "config.yaml"), "utf8");
  assert.match(config, /    exclude:\n      - skill\/beta-skill\n/);
  await step("wait for authoritative pending drift", () => {}, async () => {
    await locate(terminal, "~ 1 out of sync").unique().expect();
  });

  await step("offer sync before quitting with pending drift", () => terminal.write("q"), async () => {
    await locate(terminal, "pending changes not synced").expect();
  });
  await step("quit without syncing", () => terminal.write("n"), async () => {
    assert.equal((await waitForExit(terminal)).exitCode, 0);
  });
});

journey.test("the accepted Source → Kind grouping survives restart", async (terminal) => {
  await step("restored grouping without status buckets", () => {}, async () => {
    // The persisted Source → Kind grouping is restored: the source header
    // carries its kind sub-section and each resource renders exactly once.
    await locate(terminal, /fixture \/ skills/).unique().expect();
    await locate(terminal, /Skills\s+\(\d+\)/).unique().expect();
    await locate(terminal, "alpha-skill").expect();
    await locate(terminal, "beta-skill").expect();
    await locate(terminal, /Enabled\s+\(/).expect({ not: true });
    // Quit asks to sync only once the drift assessment has settled.
    await locate(terminal, /~ \d+ out of sync/).unique().expect({ timeout: 30_000 });
  });
  await step("offer sync before quitting with pending drift", () => terminal.write("q"), async () => {
    await locate(terminal, "pending changes not synced").expect();
  });
  await step("quit without syncing", () => terminal.write("n"), async () => {
    assert.equal((await waitForExit(terminal)).exitCode, 0);
  });
});

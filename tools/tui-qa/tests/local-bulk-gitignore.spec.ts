import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { after, before } from "node:test";

import { defineFixture, locate, step, waitForExit } from "./helpers.js";

// spec: tui-interactions/personal-local-writes-are-git-ignored-at-once
// spec: tui-interactions/scope-direction-confirmation-and-cancellation
const journey = defineFixture("local-bulk-gitignore", ["tui"]);
before(journey.setup);
after(journey.cleanup);

journey.test("a local bulk toggle git-ignores the personal-local layer before any sync", async (terminal) => {
  const fixture = journey.current();
  const localConfig = path.join(fixture.repo, ".agents", "config.local.yaml");
  const gitignorePath = path.join(fixture.repo, ".gitignore");
  const projectConfig = path.join(fixture.repo, ".agents", "config.yaml");
  const projectBefore = fs.readFileSync(projectConfig);

  await step("settled fixture baseline", () => {}, async () => {
    await locate(terminal, "~ in sync").unique().expect({ timeout: 30_000 });
  });
  assert.equal(fs.existsSync(localConfig), false, "the fixture starts without a personal-local config");
  assert.equal(fs.existsSync(gitignorePath), false, "the fixture starts without a .gitignore");
  await step("select alpha-skill", () => terminal.write("G"), async () => {
    await locate(terminal, /Name:\s+alpha-skill/).unique().expect();
  });
  await step("select the Available header", () => terminal.write("k"), async () => {
    await locate(terminal, "Select a resource to see details").unique().expect();
  });
  await step("open the section bulk modal", () => terminal.write(" "), async () => {
    await locate(terminal, "Toggle all in Available").expect();
  });
  await step("choose local scope", () => terminal.write("l"), async () => {
    await locate(terminal, /scope: local/).unique().expect();
  });
  await step("choose enable", () => terminal.write("+"), async () => {
    await locate(terminal, /enable 1 resources in the local layer/).unique().expect();
  });
  await step("confirm the batch", () => terminal.write("\r"), async () => {
    await locate(terminal, "Enabled 1 resources").expect();
    await locate(terminal, "~ 1 out of sync").unique().expect({ timeout: 30_000 });
  });
  assert.match(fs.readFileSync(localConfig, "utf8"), /skill\/alpha-skill/);
  const gitignore = fs.readFileSync(gitignorePath, "utf8").split(/\r?\n/);
  for (const entry of [".agents/config.local.yaml", ".agents/lock.local.yaml"]) {
    assert.ok(gitignore.includes(entry), `the local bulk toggle did not git-ignore ${entry}:\n${gitignore.join("\n")}`);
  }

  await step("search for project-selected beta-skill", () => terminal.write("/beta-skill"), async () => {
    await locate(terminal, /Name:\s+beta-skill/).unique().expect();
  });
  await step("apply the beta-skill filter", () => terminal.write("\r"), async () => {
    await locate(terminal, /Name:\s+beta-skill/).unique().expect();
  });
  await step("select the Enabled header", () => terminal.write("k"), async () => {
    await locate(terminal, "Select a resource to see details").unique().expect();
  });
  await step("open the filtered Enabled bulk modal", () => terminal.write(" "), async () => {
    await locate(terminal, "Toggle all in Enabled").unique().expect();
    await locate(terminal, /1 matching of 1 in section · 1 eligible · 0 skipped/).unique().expect();
  });
  await step("choose local scope for beta-skill", () => terminal.write("l"), async () => {
    await locate(terminal, /scope: local/).unique().expect();
  });
  await step("choose disable for beta-skill", () => terminal.write("-"), async () => {
    await locate(terminal, /disable 1 resources in the local_exclude layer/).unique().expect();
  });
  await step("confirm the local exclusion", () => terminal.write("\r"), async () => {
    await locate(terminal, "Disabled 1 resources").expect();
    await locate(terminal, "~ 2 out of sync").unique().expect({ timeout: 30_000 });
  });
  assert.match(fs.readFileSync(localConfig, "utf8"), /exclude:\s*\n\s+- skill\/beta-skill/);
  assert.deepEqual(fs.readFileSync(projectConfig), projectBefore, "the local bulk exclusion changed the project config");

  await step("offer sync before quitting with pending drift", () => terminal.write("q"), async () => {
    await locate(terminal, "pending changes not synced").expect();
  });
  await step("quit without syncing", () => terminal.write("n"), async () => {
    assert.equal((await waitForExit(terminal)).exitCode, 0);
  });
});

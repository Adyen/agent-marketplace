import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { after, before } from "node:test";

import { defineFixture, locate, step, waitForExit } from "./helpers.js";
import { runAM } from "./process-helpers.js";

// spec: tui-interactions/global-key-disables-and-restores-an-optional-organization-default
const journey = defineFixture("org-default-toggle", ["tui"], 80, 24, {
  organization: "published-lock",
  optionalOrganization: true
});
before(journey.setup);
after(journey.cleanup);

journey.test("g excludes and restores an optional organization default", async (terminal) => {
  const fixture = journey.current();
  const userConfig = path.join(fixture.home, ".agents", "config.yaml");
  const repoConfig = path.join(fixture.repo, ".agents", "config.yaml");
  const orgConfig = fixture.orgConfigPath;
  const orgLock = fixture.orgLockPath;
  assert.ok(orgConfig);
  assert.ok(orgLock);
  const repoBefore = fs.readFileSync(repoConfig);
  const orgBefore = fs.readFileSync(orgConfig);
  const lockBefore = fs.readFileSync(orgLock);

  await step("settled optional organization default", () => {}, async () => {
    await locate(terminal, "org-review").expect();
    await locate(terminal, "~ in sync").unique().expect({ timeout: 30_000 });
  });

  await step("search for org-review", () => terminal.write("/org-review"), async () => {
    await locate(terminal, /Name:\s+org-review/).unique().expect();
  });
  await step("apply the search", () => terminal.write("\r"), async () => {
    await locate(terminal, /Name:\s+org-review/).unique().expect();
  });
  await step("open the organization default toggle", () => terminal.write(" "), async () => {
    await locate(terminal, "User override: no").unique().expect();
    await locate(terminal, "disable for me").unique().expect();
  });
  await step("g writes the user exclusion", () => terminal.write("g"), async () => {
    await locate(terminal, /Status:\s+excluded/).unique().expect();
  });
  assert.match(fs.readFileSync(userConfig, "utf8"), /exclude:\s*\n\s+- tools\/skill\/org-review/);
  assert.deepEqual(fs.readFileSync(repoConfig), repoBefore, "the user exclusion changed repository config");
  assert.deepEqual(fs.readFileSync(orgConfig), orgBefore, "the user exclusion changed organization policy");
  assert.deepEqual(fs.readFileSync(orgLock), lockBefore, "the user exclusion changed the organization lock");

  await step("select the excluded organization default", () => terminal.write("G"), async () => {
    await locate(terminal, /Status:\s+excluded/).unique().expect();
  });
  await step("open its restore action", () => terminal.write(" "), async () => {
    await locate(terminal, "User override: yes").unique().expect();
    await locate(terminal, "restore org default").unique().expect();
  });
  await step("g clears the user exclusion", () => terminal.write("g"), async () => {
    await locate(terminal, /Status:\s+enabled/).unique().expect();
  });
  assert.doesNotMatch(fs.readFileSync(userConfig, "utf8"), /- tools\/skill\/org-review/);
  assert.deepEqual(fs.readFileSync(repoConfig), repoBefore, "restoring the default changed repository config");
  assert.deepEqual(fs.readFileSync(orgConfig), orgBefore, "restoring the default changed organization policy");
  assert.deepEqual(fs.readFileSync(orgLock), lockBefore, "restoring the default changed the organization lock");

  await step("quit", () => terminal.write("q"), async () => {
    assert.equal((await waitForExit(terminal)).exitCode, 0);
  });

  const sync = await runAM(fixture, ["sync"]);
  assert.equal(sync.status, 0, `${sync.stdout}\n${sync.stderr}`);
  const check = await runAM(fixture, ["sync", "--check"]);
  assert.equal(check.status, 0, `${check.stdout}\n${check.stderr}`);
});

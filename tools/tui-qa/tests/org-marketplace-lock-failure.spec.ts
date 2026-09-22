import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";

import { expect, test } from "@microsoft/tui-test";

import { declineLegacyMigration, defineFixture, step, waitForExit } from "./helpers.js";
import { runAM } from "./process-helpers.js";

// Reproduces the reported support case: an organization v4 config declares a
// Marketplace source but the organization acquisition lock was never shipped.
// Every surface must fail closed, name the organization lock as the owning
// layer, refuse to create it, and recover only when it is published.
const journey = defineFixture("org-marketplace-lock-failure", ["tui"], 80, 24, { organization: "missing-lock" });
test.use(journey.options);
test.beforeAll(journey.setup);
test.afterAll(journey.cleanup);

test("organization marketplace source without its lock fails closed and names the owning layer", async ({ terminal }) => {
  const fixture = journey.current();
  const orgConfigPath = fixture.orgConfigPath;
  const orgLockPath = fixture.orgLockPath;
  const publishOrgLock = fixture.publishOrgLock;
  assert.ok(orgConfigPath, "organization fixture must expose its config path");
  assert.ok(orgLockPath, "organization fixture must expose its lock path");
  assert.ok(publishOrgLock, "organization fixture must expose its lock publisher");
  assert.equal(fs.lstatSync(orgLockPath, { throwIfNoEntry: false }), undefined);

  await declineLegacyMigration(terminal);

  await step("startup surfaces the organization failure", () => {}, async () => {
    await expect(terminal.getByText(/~ drift unavailable/g)).toBeVisible();
    await expect(terminal.getByText(/! 1 errors/g)).toBeVisible();
  });

  await step("errors pane names the lock failure", () => terminal.write("e"), async () => {
    await expect(terminal.getByText(/Errors \(1\)/g)).toBeVisible();
    await expect(terminal.getByText(/v4\.marketplace-lock-required/g)).toBeVisible();
  });

  await step("close errors pane", () => terminal.write("e"), async () => {
    await expect(terminal.getByText(/Errors \(1\)/g)).not.toBeVisible();
  });

  await step("sync from the TUI records the same failure", () => terminal.write("s"), async () => {
    await expect(terminal.getByText(/! 2 errors/g)).toBeVisible({ timeout: 30_000 });
  });

  await step("quit cleanly from the failed state", () => terminal.write("q"), async () => {
    assert.equal((await waitForExit(terminal)).exitCode, 0);
  });

  // The CLI contract users hit first: fail closed, name the organization lock,
  // and never silently create it or mutate authored configs. These runs use the
  // event-loop-free runner so the marketplace stub stays available to the later
  // recovery steps.
  const failedSync = await runAM(fixture, ["sync"]);
  const failedOutput = `${failedSync.stdout}\n${failedSync.stderr}`;
  assert.equal(failedSync.status, 1, failedOutput);
  assert.match(failedOutput, /Errors\s+1/);
  assert.match(failedOutput, /v4\.marketplace-lock-required/);
  assert.ok(failedOutput.includes(orgLockPath), `sync must name the organization lock ${orgLockPath}:\n${failedOutput}`);
  assert.equal(fs.lstatSync(orgLockPath, { throwIfNoEntry: false }), undefined, "sync must not create the organization lock");
  assert.equal(fs.readFileSync(orgConfigPath, "utf8"), fixture.expectedOrgConfig, "sync must not rewrite the organization config");
  assert.equal(
    fs.readFileSync(path.join(fixture.repo, ".agents", "config.yaml"), "utf8"),
    fixture.expectedRepoConfig,
    "sync must not rewrite the repo config"
  );

  const failedCheck = await runAM(fixture, ["sync", "--check"]);
  assert.equal(failedCheck.status, 1, `${failedCheck.stdout}\n${failedCheck.stderr}`);

  // The reported self-help attempt: delete every personal layer, re-init, and
  // re-sync. An organization-owned failure must survive all of it unchanged.
  const repoConfigPath = path.join(fixture.repo, ".agents", "config.yaml");
  fs.rmSync(path.join(fixture.home, ".agents"), { recursive: true, force: true });
  fs.rmSync(fixture.cacheDir, { recursive: true, force: true });
  fs.rmSync(fixture.stateDir, { recursive: true, force: true });
  fs.rmSync(path.join(fixture.repo, ".agents"), { recursive: true, force: true });
  fs.rmSync(path.join(fixture.repo, ".factory"), { recursive: true, force: true });
  fs.mkdirSync(path.dirname(repoConfigPath), { recursive: true, mode: 0o700 });
  fs.writeFileSync(repoConfigPath, fixture.expectedRepoConfig, { encoding: "utf8", mode: 0o600 });

  const init = await runAM(fixture, ["init", "--harnesses", "factory"]);
  assert.equal(init.status, 0, `${init.stdout}\n${init.stderr}`);
  assert.match(init.stdout, /Initialized user personal config/);

  const wipedSync = await runAM(fixture, ["sync"]);
  const wipedOutput = `${wipedSync.stdout}\n${wipedSync.stderr}`;
  assert.equal(wipedSync.status, 1, wipedOutput);
  assert.match(wipedOutput, /v4\.marketplace-lock-required/);
  assert.ok(
    wipedOutput.includes(orgLockPath),
    `the failure must still name the organization lock ${orgLockPath} after a user-side wipe:\n${wipedOutput}`
  );

  // Publishing the organization lock (the fleet-side fix) recovers the same
  // machine without reinstalling anything.
  publishOrgLock();
  const recoveredSync = await runAM(fixture, ["sync"]);
  const recoveredOutput = `${recoveredSync.stdout}\n${recoveredSync.stderr}`;
  assert.equal(recoveredSync.status, 0, recoveredOutput);
  assert.match(recoveredOutput, /Errors\s+0/);
  const betaSkill = path.join(fixture.repo, ".factory", "skills", "beta-skill");
  assert.equal(fs.lstatSync(betaSkill).isSymbolicLink(), true, "recovered sync must materialize beta-skill");
  assert.equal(fs.readFileSync(path.join(betaSkill, "SKILL.md"), "utf8"), fixture.expectedBetaSkillContent);
  assert.equal(
    fs.lstatSync(path.join(fixture.repo, ".factory", "skills", "alpha-skill"), { throwIfNoEntry: false }),
    undefined,
    "only the selected beta-skill may materialize"
  );

  const recoveredCheck = await runAM(fixture, ["sync", "--check"]);
  assert.equal(recoveredCheck.status, 0, `${recoveredCheck.stdout}\n${recoveredCheck.stderr}`);
});

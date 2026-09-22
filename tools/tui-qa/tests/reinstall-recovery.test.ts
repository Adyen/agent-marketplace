import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { test } from "node:test";

import { createFixture } from "./fixture.js";
import { runNonTTY } from "./process-helpers.js";

// The reinstall support case without an organization Marketplace layer: a user
// who deletes every personal and generated layer, re-initializes, and syncs
// must land on a fully recovered workspace. The second half pins the reported
// "sync fixed it" case: a missing writable repository lock alone must be
// reconciled by plain sync.
test("deleting every personal layer then re-initializing recovers the workspace", async (t) => {
  const fixture = await createFixture("reinstall-recovery");
  t.after(() => {
    fixture.cleanup();
  });

  const betaSkill = path.join(fixture.repo, ".factory", "skills", "beta-skill");
  const repoConfigPath = path.join(fixture.repo, ".agents", "config.yaml");
  const repoLockPath = path.join(fixture.repo, ".agents", "lock.yaml");
  const userConfigPath = path.join(fixture.home, ".agents", "config.yaml");
  assert.equal(fs.lstatSync(betaSkill).isSymbolicLink(), true, "fixture baseline must be materialized");
  assert.ok(fs.existsSync(fixture.cacheDir), "fixture baseline must have warmed the catalog cache");

  // Wipe everything a reinstall touches: user config and lock, registry and
  // discovery caches, durable state, the repo config directory, and every
  // generated harness output.
  fixture.resetPersonalState();
  assert.equal(fs.lstatSync(userConfigPath, { throwIfNoEntry: false }), undefined);
  assert.equal(fs.lstatSync(betaSkill, { throwIfNoEntry: false }), undefined);

  const init = runNonTTY(fixture, ["init", "--harnesses", "factory"], 30_000);
  assert.equal(init.status, 0, `${init.stdout}\n${init.stderr}`);
  const userConfig = fs.readFileSync(userConfigPath, "utf8");
  assert.match(userConfig, /apiVersion: agent-marketplace\/v4/);
  assert.match(userConfig, /harnesses:/);
  assert.match(userConfig, /- factory/);

  const recoveredSync = runNonTTY(fixture, ["sync"], 30_000);
  const recoveredOutput = `${recoveredSync.stdout}\n${recoveredSync.stderr}`;
  assert.equal(recoveredSync.status, 0, recoveredOutput);
  assert.equal(fs.lstatSync(betaSkill).isSymbolicLink(), true, "recovered sync must materialize beta-skill");
  assert.equal(fs.readFileSync(path.join(betaSkill, "SKILL.md"), "utf8"), fixture.expectedBetaSkillContent);
  assert.equal(
    fs.lstatSync(path.join(fixture.repo, ".factory", "skills", "alpha-skill"), { throwIfNoEntry: false }),
    undefined,
    "only the selected beta-skill may materialize"
  );
  assert.ok(fs.existsSync(repoLockPath), "recovered sync must recreate the repository lock");
  assert.match(fs.readFileSync(repoLockPath, "utf8"), /scope: repo/);

  const recoveredCheck = runNonTTY(fixture, ["sync", "--check"], 30_000);
  assert.equal(recoveredCheck.status, 0, `${recoveredCheck.stdout}\n${recoveredCheck.stderr}`);

  // The "sync fixed it" case: losing only the writable repository lock must be
  // reconciled by plain sync, without --locked refusing or the workspace
  // drifting.
  fs.rmSync(repoLockPath, { force: true });
  const locklessSync = runNonTTY(fixture, ["sync"], 30_000);
  assert.equal(locklessSync.status, 0, `${locklessSync.stdout}\n${locklessSync.stderr}`);
  assert.ok(fs.existsSync(repoLockPath), "sync must recreate the missing repository lock");
  assert.equal(fs.lstatSync(betaSkill).isSymbolicLink(), true, "beta-skill must survive lock reconciliation");
  const locklessCheck = runNonTTY(fixture, ["sync", "--check"], 30_000);
  assert.equal(locklessCheck.status, 0, `${locklessCheck.stdout}\n${locklessCheck.stderr}`);
});

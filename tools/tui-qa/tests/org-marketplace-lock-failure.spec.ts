import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";

import { expect, test } from "@microsoft/tui-test";

import { declineLegacyMigration, defineFixture, step, waitForExit } from "./helpers.js";
import { runAM } from "./process-helpers.js";

// Reproduces the reported support case: an organization v4 config declares an
// exact Marketplace resource but the organization acquisition lock was never
// shipped. Consumer paths must acquire verified evidence transiently, keep the
// managed layer untouched, and remain recoverable after personal state is
// deleted.
const journey = defineFixture("org-marketplace-lock-failure", ["tui"], 80, 24, { organization: "missing-lock" });
test.use(journey.options);
test.beforeAll(journey.setup);
test.afterAll(journey.cleanup);

test("a missing organization marketplace lock is acquired transiently without policy writes", async ({ terminal }) => {
  const fixture = journey.current();
  const orgConfigPath = fixture.orgConfigPath;
  const orgLockPath = fixture.orgLockPath;
  assert.ok(orgConfigPath, "organization fixture must expose its config path");
  assert.ok(orgLockPath, "organization fixture must expose its lock path");
  assert.equal(fs.lstatSync(orgLockPath, { throwIfNoEntry: false }), undefined);

  await declineLegacyMigration(terminal);

  await step("cold startup resolves the organization marketplace", () => {}, async () => {
    await expect(terminal.getByText("org-review", { strict: false })).toBeVisible({ timeout: 30_000 });
    await expect(terminal.getByText(/~ \d+ out of sync/g)).toBeVisible();
  });

  await step("startup has no lock failure", () => {}, async () => {
    await expect(terminal.getByText(/! \d+ errors/g)).not.toBeVisible();
    await expect(terminal.getByText(/~ drift unavailable/g)).not.toBeVisible();
    assert.equal(fs.lstatSync(orgLockPath, { throwIfNoEntry: false }), undefined, "TUI load must not create the organization lock");
  });

  await step("sync from the TUI materializes transient resources", () => terminal.write("s"), async () => {
    await expect(terminal.getByText(/~ in sync/g)).toBeVisible({ timeout: 30_000 });
  });

  await step("update from the TUI reuses transient organization evidence", () => terminal.write("u"), async () => {
    await expect(terminal.getByText("Update complete", { strict: false })).toBeVisible({ timeout: 30_000 });
    await expect(terminal.getByText(/~ in sync/g)).toBeVisible();
  });

  await step("quit cleanly", () => terminal.write("q"), async () => {
    assert.equal((await waitForExit(terminal)).exitCode, 0);
  });

  const betaSkill = path.join(fixture.repo, ".factory", "skills", "beta-skill");
  const orgSkill = path.join(fixture.home, ".factory", "skills", "org-review");
  assert.equal(fs.lstatSync(betaSkill).isSymbolicLink(), true, "TUI sync must materialize beta-skill");
  assert.equal(fs.lstatSync(orgSkill).isSymbolicLink(), true, "TUI sync must materialize org-review");
  assert.equal(fs.readFileSync(path.join(orgSkill, "SKILL.md"), "utf8"), fixture.expectedOrgSkillContent);
  assert.equal(
    fixture.marketplaceAcquisitionDataRequestCount?.(),
    4,
    `TUI startup, sync, update, and reload must reuse one invocation-scoped transient acquisition: ${JSON.stringify(
      fixture.marketplaceAcquisitionDataRequestPaths?.()
    )}`
  );
  assert.equal(fs.lstatSync(orgLockPath, { throwIfNoEntry: false }), undefined, "TUI sync must not create the organization lock");
  assert.equal(fs.readFileSync(orgConfigPath, "utf8"), fixture.expectedOrgConfig, "sync must not rewrite the organization config");
  assert.equal(
    fs.readFileSync(path.join(fixture.repo, ".agents", "config.yaml"), "utf8"),
    fixture.expectedRepoConfig,
    "sync must not rewrite the repo config"
  );

  // The CLI path must independently reacquire and remain idempotent.
  const sync = await runAM(fixture, ["sync"]);
  const syncOutput = `${sync.stdout}\n${sync.stderr}`;
  assert.equal(sync.status, 0, syncOutput);
  assert.match(syncOutput, /Errors\s+0/);
  assert.equal(fs.lstatSync(orgLockPath, { throwIfNoEntry: false }), undefined, "CLI sync must not create the organization lock");

  // CLI check independently reacquires transient evidence without publishing
  // organization policy.
  const requestsBeforeCheck = fixture.marketplaceAcquisitionDataRequestCount?.() ?? 0;
  const check = await runAM(fixture, ["sync", "--check"]);
  const checkOutput = `${check.stdout}\n${check.stderr}`;
  assert.equal(check.status, 0, checkOutput);
  assert.doesNotMatch(checkOutput, /v4\.marketplace-lock-required/);
  assert.ok(
    (fixture.marketplaceAcquisitionDataRequestCount?.() ?? 0) > requestsBeforeCheck,
    "sync --check must acquire transient organization evidence"
  );
  assert.equal(fs.lstatSync(orgLockPath, { throwIfNoEntry: false }), undefined, "sync --check must not create the organization lock");

  // The reported self-help attempt: delete every personal layer, re-init, and
  // re-sync. The transient organization resource must recover without a
  // fleet-side policy write.
  fixture.resetPersonalState();

  const init = await runAM(fixture, ["init", "--harnesses", "factory"]);
  assert.equal(init.status, 0, `${init.stdout}\n${init.stderr}`);
  assert.match(init.stdout, /Initialized user personal config/);

  const wipedSync = await runAM(fixture, ["sync"]);
  const wipedOutput = `${wipedSync.stdout}\n${wipedSync.stderr}`;
  assert.equal(wipedSync.status, 0, wipedOutput);
  assert.match(wipedOutput, /Errors\s+0/);
  assert.equal(fs.lstatSync(betaSkill).isSymbolicLink(), true, "wiped sync must rematerialize beta-skill");
  assert.equal(fs.lstatSync(orgSkill).isSymbolicLink(), true, "wiped sync must rematerialize org-review");
  assert.equal(fs.readFileSync(path.join(betaSkill, "SKILL.md"), "utf8"), fixture.expectedBetaSkillContent);
  assert.equal(fs.lstatSync(orgLockPath, { throwIfNoEntry: false }), undefined, "recovery must not publish the organization lock");
  assert.equal(fs.readFileSync(orgConfigPath, "utf8"), fixture.expectedOrgConfig, "recovery must preserve organization config bytes");
});

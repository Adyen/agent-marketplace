import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";

import { expect, test } from "@microsoft/tui-test";

import { defineFixture, step, waitForExit } from "./helpers.js";
import { runAM, write } from "./process-helpers.js";

// spec: tui-feedback/sync-recovery-shortcut-remains-visible
const journey = defineFixture("missing-exact-snapshot-recovery", ["tui"]);
test.use(journey.options);
test.beforeAll(async () => {
  await journey.setup();
  const fixture = journey.current();
  write(path.join(fixture.home, ".agents", "config.yaml"), "apiVersion: agent-marketplace/v4\nharnesses: [factory]\nsources: {}\n");
  fs.renameSync(path.join(fixture.cacheDir, "exact-git", "snapshots"), path.join(fixture.root, "saved-exact-snapshots"));
});
test.afterAll(journey.cleanup);

test("missing exact snapshot warns and the sync key restores the locked content", async ({ terminal }) => {
  const fixture = journey.current();
  const lockPath = path.join(fixture.repo, ".agents", "lock.yaml");
  const lockBefore = fs.readFileSync(lockPath);
  const configPath = path.join(fixture.repo, ".agents", "config.yaml");
  const configBefore = fs.readFileSync(configPath);

  await step("missing snapshot offers sync rather than an error", () => {}, async () => {
    await expect(terminal.getByText("~ drift unavailable")).toBeVisible();
    await expect(terminal.getByText("s sync", { strict: false })).toBeVisible();
    await expect(terminal.getByText(/! \d+ errors/g)).not.toBeVisible();
    assert.deepEqual(fs.readFileSync(lockPath), lockBefore);
    assert.deepEqual(fs.readFileSync(configPath), configBefore);
    assert.equal(fs.readFileSync(path.join(fixture.repo, ".factory", "skills", "beta-skill", "SKILL.md"), "utf8"), fixture.expectedBetaSkillContent, "inspection removed or changed installed content");
    assert.ok(!fs.existsSync(path.join(fixture.cacheDir, "exact-git", "snapshots")), "inspection acquired the missing snapshot");
  });

  await step("diagnostics explain the recovery", () => terminal.write("e"), async () => {
    await expect(terminal.getByText(/Warnings \([1-9]\d*\)/g)).toBeVisible();
    await expect(terminal.getByText("press s to sync", { strict: false })).toBeVisible();
    await expect(terminal.getByText(/Errors \([1-9]\d*\)/g)).not.toBeVisible();
  });

  await step("return to the recovery shortcut", () => terminal.write("e"), async () => {
    await expect(terminal.getByText("s sync", { strict: false })).toBeVisible();
  });

  await step("explicit sync restores verified state", () => terminal.write("s"), async () => {
    await expect(terminal.getByText("~ in sync")).toBeVisible({ timeout: 30_000 });
    await expect(terminal.getByText("beta-skill", { strict: false })).toBeVisible();
    await expect(terminal.getByText(/! \d+ errors/g)).not.toBeVisible();
  });
  assert.deepEqual(fs.readFileSync(lockPath), lockBefore, "sync changed the acquisition lock");
  assert.deepEqual(fs.readFileSync(configPath), configBefore, "sync changed configuration");
  assert.equal(fs.readFileSync(path.join(fixture.repo, ".factory", "skills", "beta-skill", "SKILL.md"), "utf8"), fixture.expectedBetaSkillContent);
  const checked = await runAM(fixture, ["sync", "--check"]);
  assert.equal(checked.status, 0, checked.stdout + checked.stderr);

  await step("warning clears after successful sync", () => terminal.write("e"), async () => {
    await expect(terminal.getByText("press s to sync", { strict: false })).not.toBeVisible();
    await expect(terminal.getByText(/Warnings \([1-9]\d*\)/g)).not.toBeVisible();
  });

  await step("return from diagnostics", () => terminal.write("e"), async () => {
    await expect(terminal.getByText("~ in sync")).toBeVisible();
  });
  await step("quit", () => terminal.write("q"), async () => {
    assert.equal((await waitForExit(terminal)).exitCode, 0);
  });
});

import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";

import { expect, test } from "@microsoft/tui-test";

import { declineLegacyMigration, defineFixture, step, waitForExit } from "./helpers.js";
import { runNonTTY } from "./process-helpers.js";

const journey = defineFixture("local-source-toggle", ["tui"]);
test.use(journey.options);
test.beforeAll(journey.setup);
test.afterAll(journey.cleanup);

test("whole-source toggle and group modal change a source locally", async ({ terminal }) => {
  const fixture = journey.current();
  await declineLegacyMigration(terminal);
  const betaSkill = path.join(fixture.repo, ".factory", "skills", "beta-skill");
  const localConfig = path.join(fixture.repo, ".agents", "config.local.yaml");
  const readLocalConfig = (): string => fs.readFileSync(localConfig, "utf8");

  await step("settled fixture baseline", () => {}, async () => {
    await expect(terminal.getByText("beta-skill", { strict: false })).toBeVisible();
    await expect(terminal.getByText("alpha-skill", { strict: false })).toBeVisible();
    await expect(terminal.getByText("~ in sync")).toBeVisible();
  });
  assert.equal(fs.lstatSync(betaSkill).isSymbolicLink(), true);

  await step("select available alpha-skill", () => terminal.write("G"), async () => {
    await expect(terminal.getByText(/Name:\s+alpha-skill/g)).toBeVisible();
    await expect(terminal.getByText(/Status:\s+available/g)).toBeVisible();
  });

  const openSourceMenu = async (name: string): Promise<void> => {
    await step(`${name}: open whole-source menu`, () => terminal.write("x"), async () => {
      await expect(terminal.getByText(/Source ".+"/g)).toBeVisible();
      await expect(terminal.getByText("toggle local for every resource", { strict: false })).toBeVisible();
    });
  };

  await openSourceMenu("exclude");
  await step("exclude the whole source", () => terminal.write("e"), async () => {
    await expect(terminal.getByText(/Excluded source .+ locally \(2 resources\)/g)).toBeVisible();
    await expect(terminal.getByText(/Status:\s+excluded \(whole source, local\)/g)).toBeVisible();
    await expect(terminal.getByText("~ 1 out of sync")).toBeVisible();
  });
  const excluded = readLocalConfig();
  assert.match(excluded, /apiVersion: agent-marketplace\/v4/);
  assert.match(excluded, /exclude:\s*\n\s*-\s*['"]\*['"]/);
  assert.ok(excluded.includes(fixture.registryURL), excluded);
  assert.equal(fs.readFileSync(path.join(fixture.repo, ".agents", "config.yaml"), "utf8"), fixture.expectedRepoConfig);
  assert.equal(fs.lstatSync(betaSkill).isSymbolicLink(), true);

  await step("sync removes the excluded source", () => terminal.write("s"), async () => {
    await expect(terminal.getByText("Sync complete")).toBeVisible({ timeout: 30_000 });
    await expect(terminal.getByText("~ in sync")).toBeVisible({ timeout: 30_000 });
  });
  assert.equal(fs.lstatSync(betaSkill, { throwIfNoEntry: false }), undefined);

  await openSourceMenu("restore");
  await step("restore the whole source", () => terminal.write("e"), async () => {
    await expect(terminal.getByText(/Removed local exclusions of source/g)).toBeVisible();
    await expect(terminal.getByText(/Status:\s+available/g)).toBeVisible();
    await expect(terminal.getByText("~ 1 out of sync")).toBeVisible();
  });
  assert.ok(!readLocalConfig().includes(fixture.registryURL), readLocalConfig());

  await step("sync restores the source", () => terminal.write("s"), async () => {
    await expect(terminal.getByText("Sync complete")).toBeVisible({ timeout: 30_000 });
    await expect(terminal.getByText("~ in sync")).toBeVisible({ timeout: 30_000 });
  });
  assert.equal(fs.lstatSync(betaSkill).isSymbolicLink(), true);
  assert.equal(fs.readFileSync(path.join(betaSkill, "SKILL.md"), "utf8"), fixture.expectedBetaSkillContent);

  const alphaSkill = path.join(fixture.repo, ".factory", "skills", "alpha-skill");
  assert.equal(fs.lstatSync(alphaSkill, { throwIfNoEntry: false }), undefined);
  const openGroupModal = async (name: string, localCount: string): Promise<void> => {
    await step(`${name}: select available resource`, () => terminal.write("G"), async () => {
      await expect(terminal.getByText("alpha-skill", { strict: false })).toBeVisible();
    });
    await step(`${name}: open group modal`, () => terminal.write("x"), async () => {
      await expect(terminal.getByText(/Source ".+"/g)).toBeVisible();
      await expect(terminal.getByText(new RegExp(`Local:\\s+${localCount}`, "g"))).toBeVisible();
      await expect(terminal.getByText("toggle local for every resource", { strict: false })).toBeVisible();
    });
  };

  await openGroupModal("enable", "0/2");
  await step("enable the whole source locally", () => terminal.write("l"), async () => {
    await expect(terminal.getByText(/Enabled source .+ in local config \(2 resources\)/g)).toBeVisible();
    await expect(terminal.getByText("~ 1 out of sync")).toBeVisible();
  });
  const enabled = readLocalConfig();
  assert.match(enabled, /all: true/);
  assert.ok(enabled.includes(fixture.registryURL), enabled);
  assert.equal(fs.readFileSync(path.join(fixture.repo, ".agents", "config.yaml"), "utf8"), fixture.expectedRepoConfig);

  await step("sync installs the whole source", () => terminal.write("s"), async () => {
    await expect(terminal.getByText("Sync complete")).toBeVisible({ timeout: 30_000 });
    await expect(terminal.getByText("~ in sync")).toBeVisible({ timeout: 30_000 });
  });
  assert.equal(fs.lstatSync(alphaSkill).isSymbolicLink(), true);

  await openGroupModal("disable", "2/2");
  await step("disable the whole source locally", () => terminal.write("l"), async () => {
    await expect(terminal.getByText(/Disabled source .+ in local config \(2 resources\)/g)).toBeVisible();
    await expect(terminal.getByText("~ 1 out of sync")).toBeVisible();
  });
  assert.doesNotMatch(readLocalConfig(), /all: true/);

  await step("sync removes the locally enabled resource", () => terminal.write("s"), async () => {
    await expect(terminal.getByText("Sync complete")).toBeVisible({ timeout: 30_000 });
    await expect(terminal.getByText("~ in sync")).toBeVisible({ timeout: 30_000 });
  });
  assert.equal(fs.lstatSync(alphaSkill, { throwIfNoEntry: false }), undefined);
  assert.equal(fs.lstatSync(betaSkill).isSymbolicLink(), true);

  await step("quit", () => terminal.write("q"), async () => {
    assert.equal((await waitForExit(terminal)).exitCode, 0);
  });

  const check = runNonTTY(fixture, ["sync", "--check"], 30_000);
  assert.equal(check.status, 0, `${check.stdout}\n${check.stderr}`);
});

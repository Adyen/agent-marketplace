import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";

import { expect, test } from "@microsoft/tui-test";

import { defineFixture, step, waitForExit } from "./helpers.js";
import { runNonTTY } from "./process-helpers.js";

const journey = defineFixture("accept-user-migration", ["tui"]);
test.use(journey.options);
test.beforeAll(journey.setup);
test.afterAll(journey.cleanup);

// Brownfield first run: a legacy user root and a committed legacy repository
// root. Accepting the startup prompt converts only the user root, and the
// migrated user layer can then select a repository-declared legacy source.
test("accepting startup migration converts only the user root and keeps editing", async ({ terminal }) => {
  const fixture = journey.current();
  const userConfig = path.join(fixture.home, ".agents", "config.yaml");
  const repoConfig = path.join(fixture.repo, ".agents", "config.yaml");
  const userAlpha = path.join(fixture.home, ".factory", "skills", "alpha-skill");
  const legacyUser = fs.readFileSync(userConfig, "utf8");

  await step("offer user migration", () => {}, async () => {
    await expect(terminal.getByText("Configuration migration required", { strict: false })).toBeVisible();
  });
  // The modal closes before the asynchronous migration finishes; its
  // completion toast is the only signal that the files were written.
  await step("accept migration", () => terminal.write("m"), async () => {
    await expect(terminal.getByText("Migrated 1 config file(s)", { strict: false })).toBeVisible({ timeout: 30_000 });
  });
  await step("reload the migrated workspace", () => {}, async () => {
    await expect(terminal.getByText("beta-skill", { strict: false })).toBeVisible();
    await expect(terminal.getByText("~ in sync")).toBeVisible({ timeout: 30_000 });
  });
  assert.match(fs.readFileSync(userConfig, "utf8"), /^apiVersion: agent-marketplace\/v4$/m);
  assert.equal(fs.readFileSync(repoConfig, "utf8"), fixture.expectedRepoConfig, "startup migration touched the repository root");
  const backups = fs.readdirSync(path.dirname(userConfig)).filter((name) => name.startsWith("config.yaml.pre-v4."));
  assert.equal(backups.length, 1, `user backups: ${backups.join(", ")}`);
  assert.equal(fs.readFileSync(path.join(path.dirname(userConfig), backups[0]), "utf8"), legacyUser);

  await step("select available alpha-skill", () => terminal.write("G"), async () => {
    await expect(terminal.getByText(/Name:\s+alpha-skill/g)).toBeVisible();
    await expect(terminal.getByText(/Status:\s+available/g)).toBeVisible();
  });
  await step("open scope toggle", () => terminal.write(" "), async () => {
    await expect(terminal.getByText("toggle global")).toBeVisible();
  });
  await step("select globally in the migrated user root", () => terminal.write("g"), async () => {
    await expect(terminal.getByText(/Global\s+\(1\)/g)).toBeVisible();
    await expect(terminal.getByText(/Status:\s+enabled \(pending sync\)/g)).toBeVisible();
  });
  const migratedUser = fs.readFileSync(userConfig, "utf8");
  assert.match(migratedUser, /skill\/alpha-skill/);
  assert.doesNotMatch(migratedUser, /registries:/);
  assert.equal(fs.readFileSync(repoConfig, "utf8"), fixture.expectedRepoConfig, "global toggle touched the repository root");

  await step("sync global selection", () => terminal.write("s"), async () => {
    await expect(terminal.getByText("Sync complete")).toBeVisible({ timeout: 30_000 });
    await expect(terminal.getByText("~ in sync")).toBeVisible({ timeout: 30_000 });
  });
  assert.equal(fs.lstatSync(userAlpha).isSymbolicLink(), true);
  assert.equal(fs.readFileSync(path.join(userAlpha, "SKILL.md"), "utf8"), fixture.expectedAlphaSkillContent);

  await step("quit", () => terminal.write("q"), async () => {
    assert.equal((await waitForExit(terminal)).exitCode, 0);
  });
  const check = runNonTTY(fixture, ["sync", "--check"], 30_000);
  assert.equal(check.status, 0, `${check.stdout}\n${check.stderr}`);
});

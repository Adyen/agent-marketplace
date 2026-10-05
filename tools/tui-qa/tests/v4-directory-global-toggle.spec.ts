import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";

import { expect, test } from "@microsoft/tui-test";

import { defineFixture, step, waitForExit } from "./helpers.js";
import { runNonTTY } from "./process-helpers.js";

const journey = defineFixture("v4-directory-global-toggle", ["tui"], 80, 24, { directorySource: true });
test.use(journey.options);
test.beforeAll(journey.setup);
test.afterAll(journey.cleanup);

test("toggles a v4 directory source globally and syncs native harnesses", async ({ terminal }) => {
  const fixture = journey.current();
  const alphaPaths = [
    path.join(fixture.home, ".factory", "skills", "alpha-skill"),
    path.join(fixture.home, ".gemini", "skills", "alpha-skill"),
    path.join(fixture.home, ".config", "devin", "skills", "alpha-skill")
  ];
  const globalConfig = path.join(fixture.home, ".agents", "config.yaml");

  await step("initial directory catalog is visible", () => {}, async () => {
    await expect(terminal.getByText("alpha-skill", { strict: false })).toBeVisible();
    await expect(terminal.getByText("beta-skill", { strict: false })).toBeVisible();
  });
  await step("search for available alpha", () => terminal.write("/"), async () => {
    await expect(terminal.getByText(/\/\s{20,}/g)).toBeVisible();
  });
  await step("filter for alpha", () => terminal.write("alpha"), async () => {
    await expect(terminal.getByText("alpha-skill", { strict: false })).toBeVisible();
  });
  await step("select filtered alpha", () => terminal.write("\r"), async () => {
    await expect(terminal.getByText(/Name:\s+alpha-skill/g)).toBeVisible();
    await expect(terminal.getByText(/Status:\s+available/g)).toBeVisible();
  });
  await step("open global toggle", () => terminal.write(" "), async () => {
    await expect(terminal.getByText('"alpha-skill"', { strict: false })).toBeVisible();
    await expect(terminal.getByText("toggle global")).toBeVisible();
  });
  await step("enable global selection", () => terminal.write("g"), async () => {
    await expect(terminal.getByText(/Pending sync\s+\(1\)/g)).toBeVisible();
    await expect(terminal.getByText(/Status:\s+enabled \(pending sync\)/g)).toBeVisible();
    await expect(terminal.getByText(/~ \d+ out of sync/g)).toBeVisible();
  });
  const pendingConfig = fs.readFileSync(globalConfig, "utf8");
  assert.match(pendingConfig, /resources:\n  local:\n    include:\n      - skill\/alpha-skill/);
  assert.doesNotMatch(pendingConfig, /registries:/);

  await step("sync global selection", () => terminal.write("s"), async () => {
    await expect(terminal.getByText("Sync complete")).toBeVisible({ timeout: 30_000 });
    await expect(terminal.getByText("~ in sync")).toBeVisible({ timeout: 30_000 });
  });
  for (const generated of alphaPaths) {
    assert.equal(fs.lstatSync(generated).isSymbolicLink(), true, generated);
    assert.equal(fs.readFileSync(path.join(generated, "SKILL.md"), "utf8"), fixture.expectedAlphaSkillContent);
  }
  for (const generated of alphaPaths) {
    assert.equal(fs.lstatSync(path.join(path.dirname(generated), "beta-skill"), { throwIfNoEntry: false }), undefined);
  }
  for (const repoRoot of [".factory/skills", ".gemini/skills", ".devin/skills"]) {
    assert.equal(fs.lstatSync(path.join(fixture.repo, repoRoot, "alpha-skill"), { throwIfNoEntry: false }), undefined);
  }

  await step("open selected global toggle", () => terminal.write(" "), async () => {
    await expect(terminal.getByText('"alpha-skill"', { strict: false })).toBeVisible();
  });
  await step("disable global selection", () => terminal.write("g"), async () => {
    await expect(terminal.getByText("alpha-skill", { strict: false })).toBeVisible();
    await expect(terminal.getByText(/Status:\s+available/g)).toBeVisible();
    await expect(terminal.getByText(/Status:\s+enabled \(pending sync\)/g)).not.toBeVisible();
    await expect(terminal.getByText(/~ \d+ out of sync/g)).toBeVisible();
  });
  const disabledConfig = fs.readFileSync(globalConfig, "utf8");
  assert.match(disabledConfig, /include:\s*\[\]/);
  assert.match(disabledConfig, /exclude:\n\s+- skill\/alpha-skill/);
  assert.doesNotMatch(disabledConfig, /beta-skill/);
  await step("sync disabled selection", () => terminal.write("s"), async () => {
    await expect(terminal.getByText("Sync complete")).toBeVisible({ timeout: 30_000 });
    await expect(terminal.getByText("~ in sync")).toBeVisible({ timeout: 30_000 });
  });
  for (const generated of alphaPaths) assert.equal(fs.lstatSync(generated, { throwIfNoEntry: false }), undefined, generated);

  await step("quit", () => terminal.write("q"), async () => {
    assert.equal((await waitForExit(terminal)).exitCode, 0);
  });
  const check = runNonTTY(fixture, ["sync", "--check"], 30_000);
  assert.equal(check.status, 0, `${check.stdout}\n${check.stderr}`);
  for (const lockPath of [
    path.join(fixture.home, ".agents", "lock.yaml"),
    path.join(fixture.repo, ".agents", "lock.yaml"),
    path.join(fixture.repo, ".agents", "lock.local.yaml")
  ]) {
    assert.equal(fs.lstatSync(lockPath, { throwIfNoEntry: false }), undefined, lockPath);
  }
});

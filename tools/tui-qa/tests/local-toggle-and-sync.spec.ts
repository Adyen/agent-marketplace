import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";

import { expect, test } from "@microsoft/tui-test";

import { defineFixture, runNonTTY, step, waitForExit } from "./helpers.js";

const journey = defineFixture("local-toggle-and-sync", ["tui"]);
test.use(journey.options);
test.beforeAll(journey.setup);
test.afterAll(journey.cleanup);

test("local toggle reaches pending state before sync", async ({ terminal }) => {
  const fixture = journey.current();
  const generatedSkill = path.join(fixture.repo, ".factory", "skills", "alpha-skill");
  const generatedContent = path.join(generatedSkill, "SKILL.md");
  const localConfig = path.join(fixture.repo, ".agents", "config.local.yaml");

  await step("settled fixture baseline", () => {}, async () => {
    await expect(terminal.getByText("beta-skill", { strict: false })).toBeVisible();
    await expect(terminal.getByText("alpha-skill", { strict: false })).toBeVisible();
    await expect(terminal.getByText("~ in sync")).toBeVisible();
  });
  assert.equal(fs.existsSync(generatedSkill), false);

  await step("select available alpha-skill", () => terminal.write("G"), async () => {
    await expect(terminal.getByText(/Name:\s+alpha-skill/g)).toBeVisible();
    await expect(terminal.getByText(/Status:\s+available/g)).toBeVisible();
  });

  await step("open scope toggle", () => terminal.write(" "), async () => {
    await expect(terminal.getByText('"alpha-skill"', { strict: false })).toBeVisible();
    await expect(terminal.getByText("toggle local")).toBeVisible();
  });

  await step("toggle local", () => terminal.write("l"), async () => {
    await expect(terminal.getByText(/Local\s+\(1\)/g)).toBeVisible();
    await expect(terminal.getByText(/Status:\s+enabled \(pending sync\)/g)).toBeVisible();
    await expect(terminal.getByText("~ 1 out of sync")).toBeVisible();
  });
  assert.equal(fs.readFileSync(localConfig, "utf8"), fixture.expectedLocalConfig);
  assert.equal(fs.existsSync(generatedSkill), false);

  await step("sync pending alpha-skill", () => terminal.write("s"), async () => {
    await expect(terminal.getByText("Sync complete")).toBeVisible({ timeout: 30_000 });
    await expect(terminal.getByText("~ in sync")).toBeVisible({ timeout: 30_000 });
  });
  assert.equal(fs.lstatSync(generatedSkill).isSymbolicLink(), true);
  assert.equal(fs.readFileSync(generatedContent, "utf8"), fixture.expectedAlphaSkillContent);

  await step("quit", () => terminal.write("q"), async () => {
    assert.equal((await waitForExit(terminal)).exitCode, 0);
  });

  const check = runNonTTY(fixture, ["sync", "--check"], 30_000);
  assert.equal(check.status, 0, `${check.stdout}\n${check.stderr}`);
});

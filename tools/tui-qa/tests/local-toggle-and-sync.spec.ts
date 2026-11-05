import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { after, before } from "node:test";

import { defineFixture, locate, step, waitForExit } from "./helpers.js";
import { runNonTTY } from "./process-helpers.js";

const journey = defineFixture("local-toggle-and-sync", ["tui"]);
before(journey.setup);
after(journey.cleanup);

journey.test("local toggle reaches pending state before sync", async (terminal) => {
  const fixture = journey.current();
  const generatedSkill = path.join(fixture.repo, ".factory", "skills", "alpha-skill");
  const generatedContent = path.join(generatedSkill, "SKILL.md");
  const localConfig = path.join(fixture.repo, ".agents", "config.local.yaml");

  await step("settled fixture baseline", () => {}, async () => {
    await locate(terminal, "beta-skill").expect();
    await locate(terminal, "alpha-skill").expect();
    await locate(terminal, "~ in sync").unique().expect();
  });
  assert.equal(fs.lstatSync(generatedSkill, { throwIfNoEntry: false }), undefined);

  await step("select available alpha-skill", () => terminal.write("G"), async () => {
    await locate(terminal, /Name:\s+alpha-skill/).unique().expect();
    await locate(terminal, /Status:\s+available/).unique().expect();
  });

  await step("open scope toggle", () => terminal.write(" "), async () => {
    await locate(terminal, '"alpha-skill"').expect();
    await locate(terminal, "toggle local").unique().expect();
  });

  await step("toggle local", () => terminal.write("l"), async () => {
    await locate(terminal, /Pending sync\s+\(1\)/).unique().expect();
    await locate(terminal, /Status:\s+enabled \(pending sync\)/).unique().expect();
    await locate(terminal, "~ 1 out of sync").unique().expect();
  });
  assert.equal(fs.readFileSync(localConfig, "utf8"), fixture.expectedLocalConfig);
  assert.equal(fs.lstatSync(generatedSkill, { throwIfNoEntry: false }), undefined);

  await step("sync pending alpha-skill", () => terminal.write("s"), async () => {
    await locate(terminal, "Sync complete").unique().expect({ timeout: 30_000 });
    await locate(terminal, "~ in sync").unique().expect({ timeout: 30_000 });
  });
  assert.equal(fs.lstatSync(generatedSkill).isSymbolicLink(), true);
  assert.equal(fs.readFileSync(generatedContent, "utf8"), fixture.expectedAlphaSkillContent);

  await step("quit", () => terminal.write("q"), async () => {
    assert.equal((await waitForExit(terminal)).exitCode, 0);
  });

  const check = runNonTTY(fixture, ["sync", "--check"], 30_000);
  assert.equal(check.status, 0, `${check.stdout}\n${check.stderr}`);
});

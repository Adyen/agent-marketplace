import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { after, before } from "node:test";

import { defineFixture, locate, step, waitForExit } from "./helpers.js";
import { runNonTTY } from "./process-helpers.js";

const journey = defineFixture("local-exclude", ["tui"]);
before(journey.setup);
after(journey.cleanup);

journey.test("the toggle modal's local exclude removes a project skill only locally and can be undone", async (terminal) => {
  const fixture = journey.current();
  const repoConfig = path.join(fixture.repo, ".agents", "config.yaml");
  const localConfig = path.join(fixture.repo, ".agents", "config.local.yaml");
  const betaSkill = path.join(fixture.repo, ".factory", "skills", "beta-skill");
  const repoConfigBefore = fs.readFileSync(repoConfig);
  const localConfigText = (): string => fs.readFileSync(localConfig, "utf8");

  await step("settled fixture baseline", () => {}, async () => {
    await locate(terminal, /Enabled\s+\(1\)/).unique().expect();
    await locate(terminal, "~ in sync").unique().expect({ timeout: 30_000 });
  });
  assert.equal(fs.existsSync(localConfig), false, "the fixture starts without a personal-local config");
  await step("select beta-skill", () => terminal.write("j"), async () => {
    await locate(terminal, /Name:\s+beta-skill/).unique().expect();
  });
  await step("the toggle modal shows it selected in the project and not excluded", () => terminal.write(" "), async () => {
    await locate(terminal, /Project ✓/).unique().expect();
    await locate(terminal, "Local exclude: no").unique().expect();
  });
  await step("e excludes it locally and selects the next row", () => terminal.write("e"), async () => {
    await locate(terminal, /Available\s+\(2\)/).unique().expect({ timeout: 30_000 });
    await locate(terminal, "~ 1 out of sync").unique().expect({ timeout: 30_000 });
    await locate(terminal, /Name:\s+alpha-skill/).unique().expect();
  });
  assert.match(localConfigText(), /\n    exclude:\n      - skill\/beta-skill\n/);
  assert.deepEqual(fs.readFileSync(repoConfig), repoConfigBefore, "a local exclude changed the committed repository config");

  await step("the excluded skill reports its state", () => terminal.write("j"), async () => {
    await locate(terminal, /Name:\s+beta-skill/).unique().expect();
    await locate(terminal, /Status:\s+excluded/).unique().expect();
  });
  await step("sync removes it locally", () => terminal.write("s"), async () => {
    await locate(terminal, "~ in sync").unique().expect({ timeout: 30_000 });
    await locate(terminal, /Status:\s+excluded/).unique().expect();
  });
  assert.equal(fs.lstatSync(betaSkill, { throwIfNoEntry: false }), undefined, "sync kept the locally excluded skill");
  // Sync keeps the personal-local layer out of Git.
  const gitignore = fs.readFileSync(path.join(fixture.repo, ".gitignore"), "utf8").split(/\r?\n/);
  assert.ok(gitignore.includes(".agents/config.local.yaml"), `the personal-local config is not git-ignored:\n${gitignore.join("\n")}`);

  await step("the toggle modal shows the local exclude", () => terminal.write(" "), async () => {
    await locate(terminal, "Local exclude: yes").unique().expect();
  });
  await step("e again removes the exclude", () => terminal.write("e"), async () => {
    await locate(terminal, /Pending sync\s+\(1\)/).unique().expect({ timeout: 30_000 });
    await locate(terminal, /Name:\s+beta-skill/).unique().expect();
    await locate(terminal, /Status:\s+enabled \(pending sync\)/).unique().expect();
  });
  assert.doesNotMatch(localConfigText(), /skill\/beta-skill/, "clearing the local exclude left beta-skill in config.local.yaml");
  await step("sync installs it again", () => terminal.write("s"), async () => {
    await locate(terminal, "~ in sync").unique().expect({ timeout: 30_000 });
    await locate(terminal, /Enabled\s+\(1\)/).unique().expect();
  });
  assert.equal(fs.readFileSync(path.join(betaSkill, "SKILL.md"), "utf8"), fixture.expectedBetaSkillContent);
  assert.deepEqual(fs.readFileSync(repoConfig), repoConfigBefore, "the round trip changed the committed repository config");

  await step("quit", () => terminal.write("q"), async () => {
    assert.equal((await waitForExit(terminal)).exitCode, 0);
  });
  const check = runNonTTY(fixture, ["sync", "--check"], 30_000);
  if (check.error) throw check.error;
  assert.equal(check.status, 0, `${check.stdout}\n${check.stderr}`);
});

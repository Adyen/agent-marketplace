import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { after, before } from "node:test";

import { defineFixture, locate, step, waitForExit } from "./helpers.js";
import { runNonTTY } from "./process-helpers.js";

// spec: tui-interactions/scope-and-selection-without-write
// spec: tui-interactions/empty-selection
// spec: tui-interactions/retargeted-resource-is-pending-sync
const journey = defineFixture("harness-project-scope", ["tui"]);
before(journey.setup);
after(journey.cleanup);

journey.test("project harnesses are saved from the harness modal and retarget the workspace on sync", async (terminal) => {
  const fixture = journey.current();
  const repoConfig = path.join(fixture.repo, ".agents", "config.yaml");
  const userConfig = path.join(fixture.home, ".agents", "config.yaml");
  const factoryBeta = path.join(fixture.repo, ".factory", "skills", "beta-skill");
  const claudeBeta = path.join(fixture.repo, ".claude", "skills", "beta-skill");
  const repoBefore = fs.readFileSync(repoConfig);
  const userBefore = fs.readFileSync(userConfig);
  const assertConfigsUnchanged = (when: string): void => {
    assert.deepEqual(fs.readFileSync(repoConfig), repoBefore, `${when} changed the repository config`);
    assert.deepEqual(fs.readFileSync(userConfig), userBefore, `${when} changed the user config`);
  };

  await step("settled fixture baseline", () => {}, async () => {
    await locate(terminal, "~ in sync").unique().expect({ timeout: 30_000 });
  });
  await step("select beta-skill", () => terminal.write("j"), async () => {
    await locate(terminal, /Name:\s+beta-skill/).unique().expect();
    await locate(terminal, /Targets:\s+Factory Droid/).unique().expect();
  });
  await step("the harness modal opens on the global scope", () => terminal.write("H"), async () => {
    await locate(terminal, /Editing:\s+global/).unique().expect();
    await locate(terminal, "> [•] Factory Droid").unique().expect();
  });
  await step("p switches to the empty project scope without writing", () => terminal.write("p"), async () => {
    await locate(terminal, /Editing:\s+project/).unique().expect();
    await locate(terminal, /Project:\s+none/).unique().expect();
    await locate(terminal, "> [ ] Factory Droid").unique().expect();
  });
  await step("an empty project selection is refused", () => terminal.write("\r"), async () => {
    await locate(terminal, "select at least one harness").expect();
    await locate(terminal, /Editing:\s+project/).unique().expect();
  });
  assertConfigsUnchanged("refusing an empty selection");
  await step("toggling then cancelling writes nothing", () => terminal.write(" "), async () => {
    await locate(terminal, "> [•] Factory Droid").unique().expect();
  });
  await step("cancel the harness modal", () => terminal.write("\x1b"), async () => {
    await locate(terminal, /Editing:\s+project/).expect({ not: true });
    await locate(terminal, "~ in sync").unique().expect();
  });
  assertConfigsUnchanged("cancelling the harness modal");

  await step("reopen the harness modal on the project scope", () => terminal.write("H"), async () => {
    await locate(terminal, /Editing:\s+global/).unique().expect();
  });
  await step("switch to the project scope", () => terminal.write("p"), async () => {
    await locate(terminal, /Editing:\s+project/).unique().expect();
  });
  await step("move to Claude Code", () => terminal.write("\x1b[B"), async () => {
    await locate(terminal, "> [ ] Claude Code").unique().expect();
  });
  await step("select Claude Code for the project", () => terminal.write(" "), async () => {
    await locate(terminal, "> [•] Claude Code").unique().expect();
  });
  await step("save the project harnesses", () => terminal.write("\r"), async () => {
    await locate(terminal, "Saved project harnesses").expect();
    await locate(terminal, /Editing:\s+project/).expect({ not: true });
  });
  assert.ok(fs.readFileSync(repoConfig, "utf8").endsWith("\nharnesses:\n  - claude\n"), `the repository config did not gain the project harnesses:\n${fs.readFileSync(repoConfig, "utf8")}`);
  assert.deepEqual(fs.readFileSync(userConfig), userBefore, "saving project harnesses changed the user config");

  await step("the still-selected skill is pending sync for its new target", () => {}, async () => {
    await locate(terminal, /Pending sync\s+\(1\)/).unique().expect({ timeout: 30_000 });
    await locate(terminal, /~ \d+ out of sync/).unique().expect({ timeout: 30_000 });
    await locate(terminal, /Name:\s+beta-skill/).unique().expect();
    await locate(terminal, /Targets:\s+Claude Code \(project default\)/).unique().expect();
    await locate(terminal, /Status:\s+enabled \(pending sync\)/).unique().expect();
    await locate(terminal, /Broken\s+\(/).expect({ not: true });
  });
  await step("sync moves the skill to the new target", () => terminal.write("s"), async () => {
    await locate(terminal, "~ in sync").unique().expect({ timeout: 30_000 });
    await locate(terminal, /Enabled\s+\(1\)/).unique().expect();
  });
  assert.equal(fs.readFileSync(path.join(claudeBeta, "SKILL.md"), "utf8"), fixture.expectedBetaSkillContent);
  assert.equal(fs.lstatSync(factoryBeta, { throwIfNoEntry: false }), undefined, "sync kept the copy in the former Factory Droid target");

  await step("quit", () => terminal.write("q"), async () => {
    assert.equal((await waitForExit(terminal)).exitCode, 0);
  });
  const check = runNonTTY(fixture, ["sync", "--check"], 30_000);
  if (check.error) throw check.error;
  assert.equal(check.status, 0, `${check.stdout}\n${check.stderr}`);
});

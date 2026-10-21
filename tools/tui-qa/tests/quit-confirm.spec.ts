import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { after, before } from "node:test";

import { defineFixture, locate, step, waitForExit } from "./helpers.js";
import { runNonTTY } from "./process-helpers.js";

// spec: tui-interactions/cancel-or-stale-completion
const journey = defineFixture("quit-confirm", ["tui"]);
before(journey.setup);
after(journey.cleanup);

journey.test("quit confirmation cancels with esc and syncs before quitting with y", async (terminal) => {
  const fixture = journey.current();
  const repoConfig = path.join(fixture.repo, ".agents", "config.yaml");
  const alphaSkill = path.join(fixture.repo, ".factory", "skills", "alpha-skill");

  await step("settled fixture baseline", () => {}, async () => {
    await locate(terminal, "alpha-skill").expect();
    await locate(terminal, "~ in sync").unique().expect({ timeout: 30_000 });
  });
  await step("select available alpha-skill", () => terminal.write("G"), async () => {
    await locate(terminal, /Name:\s+alpha-skill/).unique().expect();
    await locate(terminal, /Status:\s+available/).unique().expect();
  });
  await step("open the toggle", () => terminal.write(" "), async () => {
    await locate(terminal, "toggle project").unique().expect();
  });
  await step("enable alpha-skill in the project and wait for the settled drift", () => terminal.write("p"), async () => {
    await locate(terminal, "~ 1 out of sync").unique().expect({ timeout: 30_000 });
    await locate(terminal, /Pending sync\s+\(1\)/).unique().expect();
    await locate(terminal, /Name:\s+alpha-skill/).unique().expect();
  });
  const configAfterToggle = fs.readFileSync(repoConfig);
  assert.match(configAfterToggle.toString("utf8"), /\n      - skill\/alpha-skill\n/);
  assert.equal(fs.lstatSync(alphaSkill, { throwIfNoEntry: false }), undefined, "the toggle installed alpha-skill before sync");

  const confirm = async (attempt: string): Promise<void> => {
    await step(`${attempt}: quitting with pending drift asks to sync first`, () => terminal.write("q"), async () => {
      await locate(terminal, "1 pending changes not synced. Sync+quit?").unique().expect();
      await locate(terminal, "y sync+quit · n quit · esc cancel").unique().expect();
    });
  };
  await confirm("first quit");
  await step("esc cancels and keeps the pending change and selection", () => terminal.write("\x1b"), async () => {
    await locate(terminal, "Sync+quit?").expect({ not: true });
    await locate(terminal, "~ 1 out of sync").unique().expect();
    await locate(terminal, /Pending sync\s+\(1\)/).unique().expect();
    await locate(terminal, /Name:\s+alpha-skill/).unique().expect();
  });
  assert.deepEqual(fs.readFileSync(repoConfig), configAfterToggle, "cancelling the quit changed the repository config");
  assert.equal(fs.lstatSync(alphaSkill, { throwIfNoEntry: false }), undefined, "cancelling the quit synced alpha-skill");

  await confirm("second quit");
  await step("y syncs and then quits", () => terminal.write("y"), async () => {
    assert.equal((await waitForExit(terminal, 30_000)).exitCode, 0);
  });
  assert.deepEqual(fs.readFileSync(repoConfig), configAfterToggle, "sync+quit changed the repository config");
  const installed = fs.lstatSync(alphaSkill, { throwIfNoEntry: false });
  assert.ok(installed, "sync+quit did not install alpha-skill");
  assert.equal(installed.isSymbolicLink(), true, "alpha-skill is not a symbolic link");
  assert.equal(fs.readFileSync(path.join(alphaSkill, "SKILL.md"), "utf8"), fixture.expectedAlphaSkillContent);
  assert.equal(fs.readFileSync(path.join(fixture.repo, ".factory", "skills", "beta-skill", "SKILL.md"), "utf8"), fixture.expectedBetaSkillContent);

  const check = runNonTTY(fixture, ["sync", "--check"], 30_000);
  if (check.error) throw check.error;
  assert.equal(check.status, 0, `${check.stdout}\n${check.stderr}`);
});

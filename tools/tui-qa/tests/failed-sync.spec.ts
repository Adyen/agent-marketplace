import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { after, before } from "node:test";

import { defineFixture, locate, step, waitForExit } from "./helpers.js";
import { runNonTTY } from "./process-helpers.js";

// spec: tui-error-recovery/errors-preserve-specific-remediation-and-add-safe-fallbacks
const journey = defineFixture("failed-sync", ["tui"]);
before(journey.setup);
after(journey.cleanup);

journey.test("a sync that cannot write reports the error, keeps the pending change, and recovers on retry", async (terminal) => {
  const fixture = journey.current();
  const skills = path.join(fixture.repo, ".factory", "skills");
  const alphaSkill = path.join(skills, "alpha-skill");
  const repoConfig = path.join(fixture.repo, ".agents", "config.yaml");
  const screenText = async (): Promise<string> => (await terminal.text()).replace(/\s+/g, "");

  await step("settled fixture baseline", () => {}, async () => {
    await locate(terminal, "~ in sync").unique().expect({ timeout: 30_000 });
  });
  await step("select alpha-skill", () => terminal.write("G"), async () => {
    await locate(terminal, /Name:\s+alpha-skill/).unique().expect();
  });
  await step("open its toggle modal", () => terminal.write(" "), async () => {
    await locate(terminal, "Local exclude: no").unique().expect();
  });
  await step("enable it in the project", () => terminal.write("p"), async () => {
    await locate(terminal, "~ 1 out of sync").unique().expect({ timeout: 30_000 });
  });
  assert.equal(
    fs.readFileSync(repoConfig, "utf8"),
    fixture.expectedRepoConfig.replace("      - skill/beta-skill\n", "      - skill/beta-skill\n      - skill/alpha-skill\n"),
    "enabling alpha-skill did not add it next to beta-skill"
  );

  // A read-only harness folder makes the sync's write fail.
  fs.chmodSync(skills, 0o555);
  try {
    await step("the failed sync reports an error instead of success", () => terminal.write("s"), async () => {
      await locate(terminal, /! 1 errors: materialize\.wri/).unique().expect({ timeout: 30_000 });
      await locate(terminal, "~ in sync").expect({ not: true });
      await locate(terminal, /Status:\s+enabled \(pending sync\)/).unique().expect();
    });
    assert.equal(fs.lstatSync(alphaSkill, { throwIfNoEntry: false }), undefined, "the failed sync installed alpha-skill");
    await step("the errors screen keeps the cause and gives next steps", () => terminal.write("e"), async () => {
      await locate(terminal, "Errors (1)").unique().expect();
      const screen = await screenText();
      for (const expected of ["materialize.write.error", "permissiondenied", "Run`agent-marketplacesync--check`inthisworkspace"]) {
        assert.ok(screen.includes(expected), `the sync error lost ${JSON.stringify(expected)}:\n${await terminal.text()}`);
      }
    });
    await step("close the errors screen", () => terminal.write("e"), async () => {
      await locate(terminal, "Errors (1)").expect({ not: true });
    });
  } finally {
    fs.chmodSync(skills, 0o755);
  }

  await step("retrying after the fix syncs and clears the error", () => terminal.write("s"), async () => {
    await locate(terminal, "~ in sync").unique().expect({ timeout: 30_000 });
    await locate(terminal, /! \d+ errors/).expect({ not: true });
  });
  assert.equal(fs.readFileSync(path.join(alphaSkill, "SKILL.md"), "utf8"), fixture.expectedAlphaSkillContent);

  await step("quit", () => terminal.write("q"), async () => {
    assert.equal((await waitForExit(terminal)).exitCode, 0);
  });
  const check = runNonTTY(fixture, ["sync", "--check"], 30_000);
  if (check.error) throw check.error;
  assert.equal(check.status, 0, `${check.stdout}\n${check.stderr}`);
});

import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { after, before } from "node:test";

import { defineFixture, locate, step, waitForExit } from "./helpers.js";
import { runNonTTY } from "./process-helpers.js";

// spec: resource-selection/tui-update-refuses-a-newly-missing-selection
const journey = defineFixture("update-selection-refusal", ["tui"]);
before(journey.setup);
after(journey.cleanup);

journey.test("update refuses when a selected skill was renamed upstream and writes nothing", async (terminal) => {
  const fixture = journey.current();
  const betaSkill = path.join(fixture.repo, ".factory", "skills", "beta-skill");
  const guarded = [
    path.join(fixture.repo, ".agents", "config.yaml"),
    path.join(fixture.repo, ".agents", "lock.yaml"),
    path.join(fixture.home, ".agents", "config.yaml"),
    path.join(betaSkill, "SKILL.md")
  ];

  await step("settled fixture baseline", () => {}, async () => {
    await locate(terminal, "beta-skill").expect();
    await locate(terminal, "~ in sync").unique().expect({ timeout: 30_000 });
  });
  const snapshot = guarded.map((file) => ({ file, bytes: fs.readFileSync(file), mode: fs.statSync(file).mode }));
  const link = fs.readlinkSync(betaSkill);
  fixture.pushRegistryChange(
    "skills/beta-skill-renamed/SKILL.md",
    "---\nname: beta-skill-renamed\ndescription: Renamed beta fixture skill\n---\n\n# Beta Skill Renamed\n"
  );
  fixture.pushRegistryChange("skills/beta-skill", null);

  await step("update is refused before anything is written", () => terminal.write("u"), async () => {
    await locate(terminal, /! 1 errors: update/).unique().expect({ timeout: 30_000 });
    await locate(terminal, "~ in sync").expect({ not: true });
  });
  await step("the errors screen names the selection, its remediation and the closing update line", () => terminal.write("e"), async () => {
    await locate(terminal, "Errors (1)").unique().expect();
    // The refusal wraps around a temporary registry path, so compare the
    // screen without whitespace.
    const screen = (await terminal.text()).replace(/\s+/g, "");
    for (const expected of [
      'skill"beta-skill"wasselectedbutnotfound',
      "'agent-marketplaceremove--kindskillbeta-skill'beforererunningupdate",
      "updateleftthelockandharnessoutputsunchanged"
    ]) {
      assert.ok(screen.includes(expected), `the refusal lost ${JSON.stringify(expected)}:\n${await terminal.text()}`);
    }
  });
  await step("close the errors screen", () => terminal.write("e"), async () => {
    await locate(terminal, "Errors (1)").expect({ not: true });
    await locate(terminal, /! 1 errors: update/).unique().expect();
  });
  for (const { file, bytes, mode } of snapshot) {
    assert.deepEqual(fs.readFileSync(file), bytes, `the refused update changed ${file}`);
    assert.equal(fs.statSync(file).mode, mode, `the refused update changed the mode of ${file}`);
  }
  assert.equal(fs.readlinkSync(betaSkill), link, "the refused update re-pointed the installed beta-skill");
  assert.equal(fs.lstatSync(path.join(fixture.repo, ".factory", "skills", "beta-skill-renamed"), { throwIfNoEntry: false }), undefined);

  await step("quit", () => terminal.write("q"), async () => {
    assert.equal((await waitForExit(terminal)).exitCode, 0);
  });

  // A pending dependency transaction would make this read-only check refuse.
  const check = runNonTTY(fixture, ["sync", "--check"], 30_000);
  if (check.error) throw check.error;
  assert.equal(check.status, 0, `${check.stdout}\n${check.stderr}`);
});

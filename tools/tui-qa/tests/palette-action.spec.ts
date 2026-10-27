import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { after, before } from "node:test";

import { defineFixture, locate, step, waitForExit } from "./helpers.js";
import { runNonTTY } from "./process-helpers.js";

// spec: tui-feedback/clear-search-from-the-palette
// spec: tui-feedback/prefix-action-and-excluded-controls
// spec: tui-feedback/errors-hint-only-with-diagnostics
const journey = defineFixture("palette-action", ["tui"]);
before(journey.setup);
after(journey.cleanup);

journey.test("filtered command palette actions run sync, fold and clear-search", async (terminal) => {
  const fixture = journey.current();
  const alphaSkill = path.join(fixture.repo, ".factory", "skills", "alpha-skill", "SKILL.md");
  const repoConfig = path.join(fixture.repo, ".agents", "config.yaml");
  const runAction = async (query: string, label: string, assertion: () => Promise<void>): Promise<void> => {
    await step(`the palette filters to ${label}`, () => terminal.write(`:${query}`), async () => {
      await locate(terminal, "Command palette").unique().expect();
      await locate(terminal, "1/1").unique().expect();
      await locate(terminal, `▸ ${label}`).unique().expect();
    });
    await step(`enter runs ${label}`, () => terminal.write("\r"), async () => {
      await locate(terminal, "Command palette").expect({ not: true });
      await assertion();
    });
  };

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
    // Plain drift has nothing for the errors view, so the footer offers
    // reload and sync but not errors.
    await locate(terminal, "r reload").unique().expect();
    await locate(terminal, "e errors").expect({ not: true });
  });
  await step("the palette offers no errors action for plain drift", () => terminal.write(":diagnostics"), async () => {
    await locate(terminal, "no matching actions").unique().expect();
  });
  await step("close the palette before syncing", () => terminal.write("\x1b"), async () => {
    await locate(terminal, "Command palette").expect({ not: true });
  });
  assert.equal(
    fs.readFileSync(repoConfig, "utf8"),
    fixture.expectedRepoConfig.replace("      - skill/beta-skill\n", "      - skill/beta-skill\n      - skill/alpha-skill\n"),
    "enabling alpha-skill did not add it next to beta-skill"
  );
  assert.equal(fs.existsSync(alphaSkill), false, "alpha-skill was installed before the palette sync");

  await runAction("sync", "Sync pending changes", async () => {
    await locate(terminal, "~ in sync").unique().expect({ timeout: 30_000 });
  });
  assert.equal(fs.readFileSync(alphaSkill, "utf8"), fixture.expectedAlphaSkillContent);

  await runAction("collapse", "Collapse all folds", async () => {
    await locate(terminal, /▶ Enabled\s+\(2\)/).unique().expect();
    await locate(terminal, "fixture/alpha-skill").expect({ not: true });
  });
  await runAction("expand", "Expand all folds", async () => {
    await locate(terminal, /▼ Enabled\s+\(2\)/).unique().expect();
    await locate(terminal, "fixture/alpha-skill").unique().expect();
  });

  await step("apply a search", () => terminal.write("/alpha\r"), async () => {
    await locate(terminal, "/ alpha  1 match").unique().expect();
  });
  await runAction("clear", "Clear search", async () => {
    await locate(terminal, /\/ alpha/).expect({ not: true });
    await locate(terminal, /▼ Enabled\s+\(2\)/).unique().expect();
  });
  await step("without a filter the clear-search action is absent", () => terminal.write(":clear"), async () => {
    await locate(terminal, "no matching actions").unique().expect();
    await locate(terminal, "Clear search").expect({ not: true });
  });
  await step("esc closes the palette", () => terminal.write("\x1b"), async () => {
    await locate(terminal, "Command palette").expect({ not: true });
  });
  // Modal-only scope controls are not palette actions.
  for (const control of ["global", "exclude"]) {
    await step(`the toggle modal's ${control} control is absent`, () => terminal.write(`:${control}`), async () => {
      await locate(terminal, "no matching actions").unique().expect();
    });
    await step(`close the palette after ${control}`, () => terminal.write("\x1b"), async () => {
      await locate(terminal, "Command palette").expect({ not: true });
    });
  }

  await step("quit", () => terminal.write("q"), async () => {
    assert.equal((await waitForExit(terminal)).exitCode, 0);
  });
  const check = runNonTTY(fixture, ["sync", "--check"], 30_000);
  if (check.error) throw check.error;
  assert.equal(check.status, 0, `${check.stdout}\n${check.stderr}`);
});

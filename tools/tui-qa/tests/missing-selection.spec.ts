import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { after, before } from "node:test";

import { defineFixture, locate, step, waitForExit } from "./helpers.js";
import { runNonTTY } from "./process-helpers.js";

// spec: tui-startup/optional-missing-selection-keeps-the-assessment
// spec: tui-startup/unverifiable-selections-still-fail-the-assessment
const journey = defineFixture("missing-selection", ["tui"]);
before(journey.setup);
after(journey.cleanup);

journey.test("a missing optional selection warns and keeps drift, while a missing required one fails", async (terminal) => {
  const fixture = journey.current();
  const repoConfig = path.join(fixture.repo, ".agents", "config.yaml");
  const repoLock = path.join(fixture.repo, ".agents", "lock.yaml");
  const skills = path.join(fixture.repo, ".factory", "skills");
  const registry = /\n    git: (\S+)\n/.exec(fixture.expectedRepoConfig)?.[1];
  assert.ok(registry, "the fixture repository config must declare its Git registry");
  const source = (name: string): string => `  ${name}:\n    git: ${registry}\n    ref: main\n    path: skills\n`;
  const config = (sources: string, resources: string): string => `apiVersion: agent-marketplace/v4\nsources:\n${sources}resources:\n${resources}`;
  const writeConfig = (content: string): void => fs.writeFileSync(repoConfig, content, { encoding: "utf8", mode: 0o600 });
  // The errors screen wraps diagnostics around a temporary registry path.
  const screenText = async (): Promise<string> => (await terminal.text()).replace(/\s+/g, "");
  const notFound = 'skill"gamma-skill"wasselectedbutnotfound';

  await step("settled fixture baseline", () => {}, async () => {
    await locate(terminal, "beta-skill").expect();
    await locate(terminal, "~ in sync").unique().expect({ timeout: 30_000 });
  });
  const lockBefore = fs.readFileSync(repoLock);

  writeConfig(config(source("fixture"), "  fixture:\n    include:\n      - skill/beta-skill\n      - skill/alpha-skill\n      - skill/gamma-skill\n"));
  await step("an optional selection missing upstream is a broken row and a warning, and drift is still assessed", () => terminal.write("r"), async () => {
    await locate(terminal, /Broken\s+\(1\)/).unique().expect({ timeout: 30_000 });
    await locate(terminal, "fixture/gamma-skill").unique().expect();
    await locate(terminal, "~ 1 out of sync").unique().expect();
    await locate(terminal, "⚠ 1 warnings").unique().expect();
    await locate(terminal, "e errors").unique().expect();
    await locate(terminal, /! \d+ errors/).expect({ not: true });
  });
  assert.deepEqual(fs.readFileSync(repoLock), lockBefore, "reloading a missing optional selection changed the repository lock");
  await step("the warnings screen names the missing selection and how to drop it", () => terminal.write("e"), async () => {
    await locate(terminal, "Warnings (1)").unique().expect();
    const screen = await screenText();
    for (const expected of [notFound, `'agent-marketplaceremove"gamma-skill"'`]) {
      assert.ok(screen.includes(expected), `the warning lost ${JSON.stringify(expected)}:\n${await terminal.text()}`);
    }
  });
  await step("close the warnings screen", () => terminal.write("e"), async () => {
    await locate(terminal, "Warnings (1)").expect({ not: true });
    await locate(terminal, "~ 1 out of sync").unique().expect();
  });
  await step("sync succeeds despite the missing optional selection", () => terminal.write("s"), async () => {
    await locate(terminal, "~ in sync").unique().expect({ timeout: 30_000 });
    await locate(terminal, /! \d+ errors/).expect({ not: true });
  });
  assert.equal(fs.readFileSync(path.join(skills, "alpha-skill", "SKILL.md"), "utf8"), fixture.expectedAlphaSkillContent);
  assert.equal(fs.lstatSync(path.join(skills, "gamma-skill"), { throwIfNoEntry: false }), undefined, "sync installed the missing selection");

  const lockAfterSync = fs.readFileSync(repoLock);
  writeConfig(
    config(
      source("fixture") + source("required-fixture"),
      "  fixture:\n    include:\n      - skill/beta-skill\n      - skill/alpha-skill\n  required-fixture:\n    include:\n      - skill/gamma-skill\n    required: true\n"
    )
  );
  await step("the same selection marked required fails the assessment", () => terminal.write("r"), async () => {
    await locate(terminal, /! 1 errors: skill "gamma/).unique().expect({ timeout: 30_000 });
    await locate(terminal, "fixture/gamma-skill").unique().expect();
    await locate(terminal, /~ (in sync|\d+ out of sync)/).expect({ not: true });
  });
  assert.deepEqual(fs.readFileSync(repoLock), lockAfterSync, "reloading a missing required selection changed the repository lock");
  await step("the errors screen names the missing required selection", () => terminal.write("e"), async () => {
    await locate(terminal, "Errors (1)").unique().expect();
    const screen = await screenText();
    assert.ok(screen.includes(notFound), `the error lost the missing selection:\n${await terminal.text()}`);
  });
  await step("close the errors screen", () => terminal.write("e"), async () => {
    await locate(terminal, "Errors (1)").expect({ not: true });
  });

  writeConfig(config(source("fixture"), "  fixture:\n    include:\n      - skill/beta-skill\n      - skill/alpha-skill\n"));
  await step("dropping the selection recovers the assessment", () => terminal.write("r"), async () => {
    await locate(terminal, "~ in sync").unique().expect({ timeout: 30_000 });
    await locate(terminal, /! \d+ errors/).expect({ not: true });
    await locate(terminal, "fixture/gamma-skill").expect({ not: true });
  });

  await step("quit", () => terminal.write("q"), async () => {
    assert.equal((await waitForExit(terminal)).exitCode, 0);
  });
  const check = runNonTTY(fixture, ["sync", "--check"], 30_000);
  if (check.error) throw check.error;
  assert.equal(check.status, 0, `${check.stdout}\n${check.stderr}`);
});

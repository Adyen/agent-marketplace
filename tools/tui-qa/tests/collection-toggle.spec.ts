import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { after, before } from "node:test";

import { defineFixture, locate, step, waitForExit } from "./helpers.js";
import { runNonTTY } from "./process-helpers.js";

// spec: tui-startup/verified-selected-collection-row-is-installed
// spec: tui-startup/disabling-installed-collection-removes-selection
const journey = defineFixture("collection-toggle", ["tui"], 80, 24, { collection: true });
before(journey.setup);
after(journey.cleanup);

// The include list as the Go config writer emits it (block style); any other
// shape fails the exact comparisons below rather than passing them.
const includes = (repoConfig: string): string => /\n    include:\n((?: {6}- \S+\n)*)/.exec(fs.readFileSync(repoConfig, "utf8"))?.[1] ?? "";

journey.test("enabling a collection lists it as installed and installs its member", async (terminal) => {
  const fixture = journey.current();
  const repoConfig = path.join(fixture.repo, ".agents", "config.yaml");
  const skills = path.join(fixture.repo, ".factory", "skills");

  await step("settled fixture baseline lists the collection as available", () => {}, async () => {
    await locate(terminal, /Available\s+\(2\)/).unique().expect();
    await locate(terminal, "fixture/starter").unique().expect();
    await locate(terminal, "~ in sync").unique().expect({ timeout: 30_000 });
  });
  await step("select the collection", () => terminal.write("G"), async () => {
    await locate(terminal, /Kind:\s+collection/).unique().expect();
    await locate(terminal, /Name:\s+starter/).unique().expect();
  });
  await step("open its toggle modal", () => terminal.write(" "), async () => {
    await locate(terminal, "Local exclude: no  (collection)").unique().expect();
  });
  await step("enable it in the project", () => terminal.write("p"), async () => {
    await locate(terminal, /Enabled\s+\(2\)/).unique().expect({ timeout: 30_000 });
    await locate(terminal, /Pending sync\s+\(1\)/).unique().expect();
    await locate(terminal, "~ 1 out of sync").unique().expect({ timeout: 30_000 });
    await locate(terminal, /Available\s+\(/).expect({ not: true });
    await locate(terminal, /Status:\s+enabled/).unique().expect();
  });
  assert.equal(includes(repoConfig), "      - skill/beta-skill\n      - collection/starter\n");

  await step("sync installs the collection's member", () => terminal.write("s"), async () => {
    await locate(terminal, /Enabled\s+\(3\)/).unique().expect({ timeout: 30_000 });
    await locate(terminal, "~ in sync").unique().expect({ timeout: 30_000 });
  });
  assert.equal(fs.readFileSync(path.join(skills, "alpha-skill", "SKILL.md"), "utf8"), fixture.expectedAlphaSkillContent);
  await step("quit", () => terminal.write("q"), async () => {
    assert.equal((await waitForExit(terminal)).exitCode, 0);
  });
});

journey.test("after a restart the installed collection starts enabled, and disabling it removes only its selection", async (terminal) => {
  const fixture = journey.current();
  const repoConfig = path.join(fixture.repo, ".agents", "config.yaml");
  const skills = path.join(fixture.repo, ".factory", "skills");

  await step("startup renders the selected collection as installed, not available", () => {}, async () => {
    await locate(terminal, /Enabled\s+\(3\)/).unique().expect({ timeout: 30_000 });
    await locate(terminal, "fixture/starter").unique().expect();
    await locate(terminal, /Available\s+\(/).expect({ not: true });
    await locate(terminal, "~ in sync").unique().expect({ timeout: 30_000 });
  });
  await step("select the collection", () => terminal.write("G"), async () => {
    await locate(terminal, /Kind:\s+collection/).unique().expect();
    await locate(terminal, /Status:\s+enabled/).unique().expect();
  });
  await step("open its toggle modal", () => terminal.write(" "), async () => {
    await locate(terminal, /Project ✓/).unique().expect();
  });
  await step("disabling it in the project removes the collection selection", () => terminal.write("p"), async () => {
    await locate(terminal, /Available\s+\(2\)/).unique().expect({ timeout: 30_000 });
    await locate(terminal, "~ 1 out of sync").unique().expect({ timeout: 30_000 });
    await locate(terminal, /Enabled\s+\(1\)/).unique().expect();
  });
  assert.equal(includes(repoConfig), "      - skill/beta-skill\n", "disabling the collection did not keep exactly the other selection");

  await step("sync removes the member", () => terminal.write("s"), async () => {
    await locate(terminal, "~ in sync").unique().expect({ timeout: 30_000 });
  });
  assert.equal(fs.lstatSync(path.join(skills, "alpha-skill"), { throwIfNoEntry: false }), undefined, "sync kept the removed collection's member");
  assert.equal(fs.readFileSync(path.join(skills, "beta-skill", "SKILL.md"), "utf8"), fixture.expectedBetaSkillContent);

  await step("quit", () => terminal.write("q"), async () => {
    assert.equal((await waitForExit(terminal)).exitCode, 0);
  });
  const check = runNonTTY(fixture, ["sync", "--check"], 30_000);
  if (check.error) throw check.error;
  assert.equal(check.status, 0, `${check.stdout}\n${check.stderr}`);
});

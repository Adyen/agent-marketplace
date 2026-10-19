import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { after, before } from "node:test";

import { defineFixture, locate, step, waitForExit } from "./helpers.js";
import { runNonTTY } from "./process-helpers.js";

const journey = defineFixture("v4-global-selection", ["tui"]);
before(journey.setup);
after(journey.cleanup);

// The supported startup path retains user/global editing of a source declared
// only in the repository, without conversion or edits to that repository root.
journey.test("v4 startup keeps global selection editing and repository bytes", async (terminal) => {
  const fixture = journey.current();
  const userConfig = path.join(fixture.home, ".agents", "config.yaml");
  const repoConfig = path.join(fixture.repo, ".agents", "config.yaml");
  const userAlpha = path.join(fixture.home, ".factory", "skills", "alpha-skill");
  const originalUser = fs.readFileSync(userConfig, "utf8");

  await step("load the v4 workspace without conversion", () => {}, async () => {
    await locate(terminal, "Configuration migration required").expect({ not: true });
    await locate(terminal, "beta-skill").expect();
    await locate(terminal, "~ in sync").unique().expect({ timeout: 30_000 });
  });
  assert.match(fs.readFileSync(userConfig, "utf8"), /^apiVersion: agent-marketplace\/v4$/m);
  assert.equal(fs.readFileSync(userConfig, "utf8"), originalUser, "startup changed the user root");
  assert.equal(fs.readFileSync(repoConfig, "utf8"), fixture.expectedRepoConfig, "startup touched the repository root");
  const backups = fs.readdirSync(path.dirname(userConfig)).filter((name) => name.startsWith("config.yaml.pre-v4."));
  assert.equal(backups.length, 0, `unexpected conversion backups: ${backups.join(", ")}`);

  await step("select available alpha-skill", () => terminal.write("G"), async () => {
    await locate(terminal, /Name:\s+alpha-skill/).unique().expect();
    await locate(terminal, /Status:\s+available/).unique().expect();
  });
  await step("open scope toggle", () => terminal.write(" "), async () => {
    await locate(terminal, "toggle global").unique().expect();
  });
  await step("select globally in the v4 user root", () => terminal.write("g"), async () => {
    await locate(terminal, /Pending sync\s+\(1\)/).unique().expect();
    await locate(terminal, /Status:\s+enabled \(pending sync\)/).unique().expect();
  });
  const v4User = fs.readFileSync(userConfig, "utf8");
  assert.match(v4User, /skill\/alpha-skill/);
  assert.doesNotMatch(v4User, /registries:/);
  assert.equal(fs.readFileSync(repoConfig, "utf8"), fixture.expectedRepoConfig, "global toggle touched the repository root");

  await step("sync global selection", () => terminal.write("s"), async () => {
    await locate(terminal, "Sync complete").unique().expect({ timeout: 30_000 });
    await locate(terminal, "~ in sync").unique().expect({ timeout: 30_000 });
  });
  assert.equal(fs.lstatSync(userAlpha).isSymbolicLink(), true);
  assert.equal(fs.readFileSync(path.join(userAlpha, "SKILL.md"), "utf8"), fixture.expectedAlphaSkillContent);

  await step("quit", () => terminal.write("q"), async () => {
    assert.equal((await waitForExit(terminal)).exitCode, 0);
  });
  const check = runNonTTY(fixture, ["sync", "--check"], 30_000);
  assert.equal(check.status, 0, `${check.stdout}\n${check.stderr}`);
});

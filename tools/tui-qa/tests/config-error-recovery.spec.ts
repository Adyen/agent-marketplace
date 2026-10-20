import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { after, before } from "node:test";

import { defineFixture, locate, step, waitForExit } from "./helpers.js";
import { runNonTTY } from "./process-helpers.js";

// spec: tui-error-recovery/errors-preserve-specific-remediation-and-add-safe-fallbacks
const journey = defineFixture("config-error-recovery", ["tui"]);
before(journey.setup);
after(journey.cleanup);

journey.test("a broken user config is reported with recovery steps and fixed by reloading", async (terminal) => {
  const fixture = journey.current();
  const userConfig = path.join(fixture.home, ".agents", "config.yaml");
  const betaSkill = path.join(fixture.repo, ".factory", "skills", "beta-skill");
  const workspaceFiles = [path.join(fixture.repo, ".agents", "config.yaml"), path.join(fixture.repo, ".agents", "lock.yaml"), path.join(betaSkill, "SKILL.md")];
  const assertWorkspaceUnchanged = (snapshot: { file: string; bytes: Buffer }[], link: string, when: string): void => {
    for (const { file, bytes } of snapshot) assert.deepEqual(fs.readFileSync(file), bytes, `${when} changed ${file}`);
    assert.equal(fs.readlinkSync(betaSkill), link, `${when} re-pointed the installed beta-skill`);
  };

  await step("settled fixture baseline", () => {}, async () => {
    await locate(terminal, "beta-skill").expect();
    await locate(terminal, "~ in sync").unique().expect({ timeout: 30_000 });
  });
  const goodConfig = fs.readFileSync(userConfig);
  const snapshot = workspaceFiles.map((file) => ({ file, bytes: fs.readFileSync(file) }));
  const link = fs.readlinkSync(betaSkill);
  const brokenConfig = "apiVersion: agent-marketplace/v4\nharnesses: [factory\nsources: {}\n";
  fs.writeFileSync(userConfig, brokenConfig, { encoding: "utf8", mode: 0o600 });

  await step("reload reports the configuration error instead of drift", () => terminal.write("r"), async () => {
    await locate(terminal, /! 1 errors: v4\.parse/).unique().expect({ timeout: 30_000 });
    await locate(terminal, "e errors").expect();
    await locate(terminal, "r reload").expect();
    await locate(terminal, "~ in sync").expect({ not: true });
  });
  await step("sync is refused with a recovery hint", () => terminal.write("s"), async () => {
    await locate(terminal, "configuration is invalid; press E to edit config").expect();
  });
  await step("the errors screen keeps the cause and adds config recovery steps", () => terminal.write("e"), async () => {
    await locate(terminal, "Errors (1)").unique().expect();
    await locate(terminal, "v4.parse:").expect();
    await locate(terminal, "Fix the file and field named above.").unique().expect();
    await locate(terminal, "press `r` to reload.").unique().expect();
  });
  await step("close the errors screen", () => terminal.write("e"), async () => {
    await locate(terminal, "Errors (1)").expect({ not: true });
    await locate(terminal, /! 1 errors: v4\.parse/).unique().expect();
  });
  assert.equal(fs.readFileSync(userConfig, "utf8"), brokenConfig, "the TUI rewrote the broken user config");
  assertWorkspaceUnchanged(snapshot, link, "the configuration error");

  fs.writeFileSync(userConfig, goodConfig, { mode: 0o600 });
  await step("reload after the fix clears the error and restores drift", () => terminal.write("r"), async () => {
    await locate(terminal, "~ in sync").unique().expect({ timeout: 30_000 });
    await locate(terminal, /! \d+ errors/).expect({ not: true });
  });
  assertWorkspaceUnchanged(snapshot, link, "the recovery");

  await step("quit", () => terminal.write("q"), async () => {
    assert.equal((await waitForExit(terminal)).exitCode, 0);
  });

  const check = runNonTTY(fixture, ["sync", "--check"], 30_000);
  assert.equal(check.status, 0, `${check.stdout}\n${check.stderr}`);
});

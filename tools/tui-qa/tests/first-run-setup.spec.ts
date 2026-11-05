import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { after, before } from "node:test";

import { defineFixture, locate, step, waitForExit } from "./helpers.js";
import { runNonTTY } from "./process-helpers.js";

// spec: tui-interactions/harness-options-return-after-compact-resize
const journey = defineFixture("first-run-setup", ["tui"], 80, 24, { firstRun: true });
before(journey.setup);
after(journey.cleanup);

const userConfig = (): string => path.join(journey.current().home, ".agents", "config.yaml");

journey.test("first launch offers harness setup and cancelling it writes nothing", async (terminal) => {
  await step("first-run setup opens with the detected harness pre-selected", () => {}, async () => {
    await locate(terminal, "Initialize target harnesses").unique().expect();
    await locate(terminal, "Detected installation roots are pre-selected.").unique().expect();
    await locate(terminal, "> [ ] Factory Droid").unique().expect();
    await locate(terminal, "[•] Claude Code").unique().expect();
    await locate(terminal, /~ \d+ out of sync/).unique().expect({ timeout: 30_000 });
  });
  assert.equal(fs.existsSync(userConfig()), false);

  await step("cancel setup", () => terminal.write("\x1b"), async () => {
    await locate(terminal, "Initialize target harnesses").expect({ not: true });
    await locate(terminal, "alpha-skill").expect();
  });
  assert.equal(fs.existsSync(userConfig()), false);

  await step("offer sync before quitting with pending drift", () => terminal.write("q"), async () => {
    await locate(terminal, "pending changes not synced").expect();
  });
  await step("quit without syncing", () => terminal.write("n"), async () => {
    assert.equal((await waitForExit(terminal)).exitCode, 0);
  });
  assert.equal(fs.existsSync(userConfig()), false);
});

journey.test("saved harness setup writes the user config and the first sync installs into each harness", async (terminal) => {
  const fixture = journey.current();
  const installed = (harnessRoot: string): string => path.join(fixture.repo, harnessRoot, "skills", "beta-skill");

  await step("setup reopens while no user config exists", () => {}, async () => {
    await locate(terminal, "Initialize target harnesses").unique().expect();
    await locate(terminal, /~ \d+ out of sync/).unique().expect({ timeout: 30_000 });
  });
  await step("add Factory Droid to the detected Claude Code", () => terminal.write(" "), async () => {
    await locate(terminal, "> [•] Factory Droid").unique().expect();
    await locate(terminal, "[•] Claude Code").unique().expect();
  });
  await step("move the cursor off the first choice", () => terminal.write("\x1b[B"), async () => {
    await locate(terminal, "> [•] Claude Code").unique().expect();
    await locate(terminal, "> [•] Factory Droid").expect({ not: true });
  });
  await step("compact terminal switches to the compact setup layout", () => terminal.resize(50, 12), async () => {
    await locate(terminal, "Detected targets are pre-selected.").unique().expect();
    await locate(terminal, "OpenCode").expect({ not: true });
  });
  await step("full size restores every choice with the same cursor and selection", () => terminal.resize(80, 24), async () => {
    for (const label of ["Factory Droid", "Claude Code", "Cursor", "Windsurf", "Gemini CLI", "Devin CLI", "Codex", "OpenCode"]) {
      const mark = ["Factory Droid", "Claude Code"].includes(label) ? "•" : " ";
      await locate(terminal, new RegExp(`\\[${mark}\\] ${label}\\b`)).unique().expect();
    }
    await locate(terminal, "> [•] Claude Code").unique().expect();
    await locate(terminal, "[•] Factory Droid").unique().expect();
    await locate(terminal, "> [•] Factory Droid").expect({ not: true });
  });
  await step("save the selection", () => terminal.write("\r"), async () => {
    await locate(terminal, "Saved user harnesses").unique().expect();
    await locate(terminal, "Initialize target harnesses").expect({ not: true });
  });
  assert.equal(fs.readFileSync(userConfig(), "utf8"), "apiVersion: agent-marketplace/v4\nharnesses:\n  - factory\n  - claude\n");
  assert.equal(fs.lstatSync(installed(".factory"), { throwIfNoEntry: false }), undefined);

  await step("wait for the reloaded pending drift", () => {}, async () => {
    await locate(terminal, /Pending sync\s+\(1\)/).unique().expect({ timeout: 30_000 });
    await locate(terminal, /~ \d+ out of sync/).unique().expect({ timeout: 30_000 });
  });
  await step("first sync installs the repository selection", () => terminal.write("s"), async () => {
    await locate(terminal, "Sync complete").unique().expect({ timeout: 30_000 });
    await locate(terminal, "~ in sync").unique().expect({ timeout: 30_000 });
    await locate(terminal, /Enabled\s+\(1\)/).unique().expect();
  });
  for (const harnessRoot of [".factory", ".claude"]) {
    const link = fs.lstatSync(installed(harnessRoot), { throwIfNoEntry: false });
    assert.ok(link, `${harnessRoot} beta-skill was not installed at ${installed(harnessRoot)}`);
    assert.equal(link.isSymbolicLink(), true, `${harnessRoot} beta-skill is not a symlink`);
    assert.equal(fs.readFileSync(path.join(installed(harnessRoot), "SKILL.md"), "utf8"), fixture.expectedBetaSkillContent);
  }
  assert.equal(fs.lstatSync(path.join(fixture.repo, ".factory", "skills", "alpha-skill"), { throwIfNoEntry: false }), undefined);

  await step("quit", () => terminal.write("q"), async () => {
    assert.equal((await waitForExit(terminal)).exitCode, 0);
  });
});

journey.test("a configured user skips harness setup", async (terminal) => {
  const fixture = journey.current();
  await step("launch settles in sync without the setup dialog", () => {}, async () => {
    await locate(terminal, "beta-skill").expect();
    await locate(terminal, "~ in sync").unique().expect();
    await locate(terminal, "Initialize target harnesses").expect({ not: true });
  });
  await step("quit", () => terminal.write("q"), async () => {
    assert.equal((await waitForExit(terminal)).exitCode, 0);
  });

  const check = runNonTTY(fixture, ["sync", "--check"], 30_000);
  assert.equal(check.status, 0, `${check.stdout}\n${check.stderr}`);
});

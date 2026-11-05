import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { after, before } from "node:test";

import { defineFixture, locate, step, waitForExit } from "./helpers.js";

// spec: tui-interactions/save-selected-harnesses
// spec: tui-interactions/harness-options-return-after-compact-resize
// spec: tui-interactions/slash-leaves-fixed-choice-dialogs-unchanged
const journey = defineFixture("harness-global-and-fixed-choices", ["tui"]);
before(journey.setup);
after(journey.cleanup);

const harnesses = ["Factory Droid", "Claude Code", "Cursor", "Windsurf", "Gemini CLI", "Devin CLI", "Codex", "OpenCode"];

journey.test("global harnesses are saved after a compact scroll, and fixed-choice dialogs ignore /", async (terminal) => {
  const fixture = journey.current();
  const repoConfig = path.join(fixture.repo, ".agents", "config.yaml");
  const userConfig = path.join(fixture.home, ".agents", "config.yaml");
  const claudeBeta = path.join(fixture.repo, ".claude", "skills", "beta-skill");
  const repoBefore = fs.readFileSync(repoConfig);
  const allChoicesVisible = async (selected = ["Factory Droid"]): Promise<void> => {
    for (const label of harnesses) {
      const mark = selected.includes(label) ? "•" : " ";
      await locate(terminal, new RegExp(`\\[${mark}\\] ${label}\\b`)).unique().expect();
    }
  };

  await step("settled fixture baseline", () => {}, async () => {
    await locate(terminal, "~ in sync").unique().expect({ timeout: 30_000 });
  });
  await step("the harness modal opens on the global scope", () => terminal.write("H"), async () => {
    await locate(terminal, /Editing:\s+global/).unique().expect();
    await locate(terminal, "> [•] Factory Droid").unique().expect();
    await allChoicesVisible();
  });
  await step("move to Claude Code", () => terminal.write("\x1b[B"), async () => {
    await locate(terminal, "> [ ] Claude Code").unique().expect();
  });
  await step("select Claude Code as well", () => terminal.write(" "), async () => {
    await locate(terminal, "> [•] Claude Code").unique().expect();
  });
  await step("the compact layout cannot show every choice", () => terminal.resize(50, 12), async () => {
    await locate(terminal, "global · Factory Droid").unique().expect();
    await locate(terminal, "> [•] Claude Code").unique().expect();
    await locate(terminal, /\[ \] OpenCode/).expect({ not: true });
  });
  for (const label of harnesses.slice(2)) {
    await step(`scroll the compact list to ${label}`, () => terminal.write("\x1b[B"), async () => {
      await locate(terminal, `> [ ] ${label}`).unique().expect();
    });
  }
  await step("the scrolled compact list hides Factory Droid", () => {}, async () => {
    await locate(terminal, /\[•\] Factory Droid/).expect({ not: true });
  });
  await step("full size shows every choice again with the same cursor and selection", () => terminal.resize(80, 24), async () => {
    await locate(terminal, /Editing:\s+global/).unique().expect();
    await allChoicesVisible(["Factory Droid", "Claude Code"]);
    await locate(terminal, "[•] Factory Droid").unique().expect();
    await locate(terminal, "[•] Claude Code").unique().expect();
    await locate(terminal, "> [ ] OpenCode").unique().expect();
  });
  await step("enter saves the global selection", () => terminal.write("\r"), async () => {
    await locate(terminal, /Editing:\s+global/).expect({ not: true });
    await locate(terminal, /Pending sync\s+\(1\)/).unique().expect({ timeout: 30_000 });
    await locate(terminal, /~ \d+ out of sync/).unique().expect({ timeout: 30_000 });
  });
  assert.equal(
    fs.readFileSync(userConfig, "utf8").match(/^harnesses:\n((?: {2}- [^\n]+\n)+)/m)?.[1],
    "  - factory\n  - claude\n",
    "the harness save must not select extra targets"
  );
  assert.deepEqual(fs.readFileSync(repoConfig), repoBefore, "a global harness save changed the repository config");
  await step("sync installs the project skill into Claude Code too", () => terminal.write("s"), async () => {
    await locate(terminal, "~ in sync").unique().expect({ timeout: 30_000 });
  });
  assert.ok(fs.lstatSync(claudeBeta, { throwIfNoEntry: false }), "sync did not install beta-skill into .claude/skills");

  const configsAfterSync = [repoConfig, userConfig].map((file) => fs.readFileSync(file));
  const configsUnchanged = (when: string): void => {
    assert.deepEqual([repoConfig, userConfig].map((file) => fs.readFileSync(file)), configsAfterSync, `${when} wrote config`);
    assert.equal(fs.existsSync(path.join(fixture.repo, ".agents", "config.local.yaml")), false, `${when} created a personal-local config`);
  };
  await step("select alpha-skill", () => terminal.write("G"), async () => {
    await locate(terminal, /Name:\s+alpha-skill/).unique().expect();
  });
  await step("open its toggle dialog", () => terminal.write(" "), async () => {
    await locate(terminal, "> [g]  toggle global").unique().expect();
  });
  await step("/ leaves the toggle dialog's choices unchanged", () => terminal.write("/"), async () => {
    await locate(terminal, "> [g]  toggle global").unique().expect();
    await locate(terminal, "[p]  toggle project").unique().expect();
    await locate(terminal, "↑↓ move · enter apply · esc cancel").unique().expect();
  });
  await step("arrows still move the toggle choice", () => terminal.write("\x1b[B"), async () => {
    await locate(terminal, "> [p]  toggle project").unique().expect();
  });
  await step("esc still cancels the toggle dialog", () => terminal.write("\x1b"), async () => {
    await locate(terminal, "toggle project").expect({ not: true });
    await locate(terminal, /Name:\s+alpha-skill/).unique().expect();
  });
  configsUnchanged("the toggle dialog");
  await step("open the config-editor dialog", () => terminal.write("E"), async () => {
    await locate(terminal, "> [g]  edit global config").unique().expect();
  });
  await step("/ leaves the config-editor choices unchanged", () => terminal.write("/"), async () => {
    await locate(terminal, "> [g]  edit global config").unique().expect();
    await locate(terminal, "↑↓ move · enter open · esc cancel").unique().expect();
  });
  await step("arrows still move the config-editor choice", () => terminal.write("\x1b[B"), async () => {
    await locate(terminal, "> [p]  edit project config").unique().expect();
  });
  await step("esc still cancels the config-editor dialog", () => terminal.write("\x1b"), async () => {
    await locate(terminal, "Edit Config").expect({ not: true });
    await locate(terminal, "~ in sync").unique().expect();
  });
  configsUnchanged("the config-editor dialog");

  await step("quit", () => terminal.write("q"), async () => {
    assert.equal((await waitForExit(terminal)).exitCode, 0);
  });
});

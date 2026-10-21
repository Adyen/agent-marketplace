import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { after, before } from "node:test";

import { defineFixture, locate, step, waitForExit } from "./helpers.js";
import { runNonTTY } from "./process-helpers.js";

// spec: tui-rendering/editor-running
// spec: tui-rendering/editor-exit-reloads-and-frees-writes
const journey = defineFixture("edit-config", ["tui"]);
before(journey.setup);
// The scripted editor waits for the journey to release it, so the handoff
// screen can be observed, then fails or writes the prepared config.
before(() => {
  const fixture = journey.current();
  const dir = path.join(fixture.root, "editor");
  fs.mkdirSync(dir, { recursive: true });
  const script = path.join(dir, "edit.sh");
  fs.writeFileSync(
    script,
    `#!/bin/sh\nwhile [ ! -e "${dir}/release" ]; do sleep 0.05; done\nrm -f "${dir}/release"\nif [ -e "${dir}/fail" ]; then rm -f "${dir}/fail"; exit 3; fi\ncp "${dir}/next.yaml" "$1"\n`,
    { mode: 0o755 }
  );
  fixture.env.EDITOR = script;
});
after(journey.cleanup);

journey.test("E hands the project config to $EDITOR, reloads the edit, and keeps later writes working", async (terminal) => {
  const fixture = journey.current();
  const dir = path.join(fixture.root, "editor");
  const repoConfig = path.join(fixture.repo, ".agents", "config.yaml");
  const edited = fixture.expectedRepoConfig.replace("      - skill/beta-skill\n", "      - skill/beta-skill\n      - skill/alpha-skill\n");
  fs.writeFileSync(path.join(dir, "next.yaml"), edited);
  const screenText = async (): Promise<string> => (await terminal.text()).replace(/\s+/g, "");
  const openProjectEditor = async (): Promise<void> => {
    await step("E opens the config picker", () => terminal.write("E"), async () => {
      await locate(terminal, "Edit Config").unique().expect();
      await locate(terminal, "[p]  edit project config").unique().expect();
    });
    await step("p hands the project config to the editor", () => terminal.write("p"), async () => {
      await locate(terminal, "Waiting for").expect();
      // Long temporary paths wrap and may print canonicalized, so compare
      // path suffixes without whitespace.
      const screen = await screenText();
      for (const expected of [`/${path.basename(fixture.repo)}/.agents/config.yaml`, "/editor/edit.shtoexit", "Editing/", "Waitingfor/"]) {
        assert.ok(screen.includes(expected), `the handoff screen lost ${JSON.stringify(expected)}:\n${await terminal.text()}`);
      }
      await locate(terminal, "~ in sync").expect({ not: true });
    });
  };

  await step("settled fixture baseline", () => {}, async () => {
    await locate(terminal, "~ in sync").unique().expect({ timeout: 30_000 });
  });

  fs.writeFileSync(path.join(dir, "fail"), "");
  await openProjectEditor();
  await step("a failing editor is reported with recovery guidance", () => fs.writeFileSync(path.join(dir, "release"), ""), async () => {
    await locate(terminal, "! 1 errors: exit status 3").unique().expect({ timeout: 10_000 });
  });
  assert.equal(fs.readFileSync(repoConfig, "utf8"), fixture.expectedRepoConfig, "the failed editor changed the project config");
  await step("the errors screen explains how to fix the editor", () => terminal.write("e"), async () => {
    await locate(terminal, "Errors (1)").unique().expect();
    assert.ok((await screenText()).includes("Checkthatyourconfigurededitorisinstalled"), `the editor error lost its guidance:\n${await terminal.text()}`);
  });
  await step("close the errors screen", () => terminal.write("e"), async () => {
    await locate(terminal, "Errors (1)").expect({ not: true });
  });

  await openProjectEditor();
  await step("the edit is reloaded as soon as the editor exits", () => fs.writeFileSync(path.join(dir, "release"), ""), async () => {
    await locate(terminal, /Pending sync\s+\(1\)/).unique().expect({ timeout: 30_000 });
    await locate(terminal, "~ 1 out of sync").unique().expect({ timeout: 30_000 });
    await locate(terminal, /! \d+ errors/).expect({ not: true });
  });
  assert.equal(fs.readFileSync(repoConfig, "utf8"), edited);

  await step("select alpha-skill", () => terminal.write("G"), async () => {
    await locate(terminal, /Name:\s+alpha-skill/).unique().expect();
  });
  await step("open its toggle modal", () => terminal.write(" "), async () => {
    await locate(terminal, /Project ✓/).unique().expect();
  });
  await step("a config write after the editor still applies", () => terminal.write("p"), async () => {
    await locate(terminal, /Available\s+\(1\)/).unique().expect({ timeout: 30_000 });
    await locate(terminal, "~ in sync").unique().expect({ timeout: 30_000 });
  });
  assert.notEqual(fs.readFileSync(repoConfig, "utf8"), edited, "the toggle after the editor wrote nothing");

  await step("quit", () => terminal.write("q"), async () => {
    assert.equal((await waitForExit(terminal)).exitCode, 0);
  });
  const check = runNonTTY(fixture, ["sync", "--check"], 30_000);
  if (check.error) throw check.error;
  assert.equal(check.status, 0, `${check.stdout}\n${check.stderr}`);
});

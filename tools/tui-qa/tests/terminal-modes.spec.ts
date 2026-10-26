import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import process from "node:process";
import { after, before } from "node:test";

import { defineFixture, locate, step, type TestTerminal, waitForExit } from "./helpers.js";
import { readRegularFile, targetPID } from "./process-helpers.js";

// spec: tui-mouse-navigation/wheel-reporting-is-enabled
// spec: tui-mouse-navigation/exiting-restores-terminal-mouse-modes
const journey = defineFixture("terminal-modes", ["tui"], 50, 12);
before(journey.setup);
after(journey.cleanup);

const modes = async (terminal: TestTerminal) => {
  const state = await terminal.state();
  return {
    mouse: state.mouse_mode,
    alternateScreen: state.modes.alternate_screen,
    cursorVisible: state.cursor.visible,
    bracketedPaste: state.modes.bracketed_paste
  };
};
const active = { mouse: "drag", alternateScreen: true, cursorVisible: false, bracketedPaste: true };
const restored = { mouse: "none", alternateScreen: false, cursorVisible: true, bracketedPaste: false };

for (const exit of ["q", "Ctrl+C", "SIGINT", "SIGTERM"] as const) {
  journey.test(`${exit} restores terminal modes before the PTY session closes`, async (terminal) => {
    await step("settled TUI owns the terminal with cell-motion, not all-motion, reporting", () => {}, async () => {
      await locate(terminal, "~ in sync").unique().expect({ timeout: 30_000 });
      assert.deepEqual(await modes(terminal), active);
    });
    await step(`exit through ${exit}`, async () => {
      if (exit === "SIGINT" || exit === "SIGTERM") {
        process.kill(await targetPID(journey.current()), exit);
      } else {
        await terminal.write(exit === "q" ? "q" : "\u0003");
      }
    }, async () => {
      const result = await waitForExit(terminal);
      assert.equal(result.exitCode, 0);
      assert.equal(result.signal, null, "the signal must be handled, not kill the process");
      // Read before runJourney's cleanup; terminal.close() must not repair the result.
      assert.deepEqual(await modes(terminal), restored);
    });
  });
}

journey.test("a failed sync-and-quit returns its error with terminal modes restored", async (terminal) => {
  const fixture = journey.current();
  const skills = path.join(fixture.repo, ".factory", "skills");
  const alpha = path.join(skills, "alpha-skill");
  const config = path.join(fixture.repo, ".agents", "config.yaml");
  const beta = path.join(skills, "beta-skill", "SKILL.md");
  await step("settled TUI owns the terminal before the failing operation", () => {}, async () => {
    await locate(terminal, "~ in sync").unique().expect({ timeout: 30_000 });
    assert.deepEqual(await modes(terminal), active);
  });
  const betaBefore = readRegularFile(beta);
  await step("select available alpha-skill", () => terminal.write("G"), async () => {
    await locate(terminal, /Name:\s+alpha-skill/).unique().expect();
  });
  await step("open the project toggle", () => terminal.write(" "), async () => {
    await locate(terminal, "[p] project").unique().expect();
  });
  await step("select alpha-skill without syncing", () => terminal.write("p"), async () => {
    await locate(terminal, "~ 1 out of sync").unique().expect({ timeout: 30_000 });
  });
  const configAfterToggle = readRegularFile(config);
  assert.match(configAfterToggle.toString("utf8"), /\n      - skill\/alpha-skill\n/);
  const mode = fs.statSync(skills).mode & 0o7777;
  fs.chmodSync(skills, 0o555);
  try {
    await step("quit asks to sync the pending change", () => terminal.write("q"), async () => {
      await locate(terminal, "Sync+quit?").unique().expect();
    });
    await step("the failed sync exits nonzero with a restored terminal", () => terminal.write("y"), async () => {
      const result = await waitForExit(terminal, 30_000);
      assert.equal(result.exitCode, 1, "the sync failure must reach the CLI exit status");
      assert.equal(result.signal, null);
      assert.deepEqual(await modes(terminal), restored);
      await locate(terminal, "materialize.write.error").expect();
    });
    assert.equal(fs.lstatSync(alpha, { throwIfNoEntry: false }), undefined, "the failed sync installed alpha-skill");
    assert.deepEqual(readRegularFile(beta), betaBefore, "the failed sync changed installed beta-skill");
    assert.deepEqual(readRegularFile(config), configAfterToggle, "sync-and-quit changed the authored selection");
  } finally {
    fs.chmodSync(skills, mode);
  }
});

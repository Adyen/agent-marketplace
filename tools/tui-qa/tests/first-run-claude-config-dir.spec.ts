import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { after, before } from "node:test";

import { defineFixture, locate, step, waitForExit } from "./helpers.js";

// spec: claude-native-configuration/harness-detection-recognizes-the-configuration-directory
const journey = defineFixture("first-run-claude-config-dir", ["tui"], 80, 24, { firstRun: true });
before(async () => {
  await journey.setup();
  const fixture = journey.current();
  fixture.env.CLAUDE_CONFIG_DIR = path.join(fixture.home, ".claude-account");
  fs.renameSync(path.join(fixture.home, ".claude"), fixture.env.CLAUDE_CONFIG_DIR);
});
after(journey.cleanup);

journey.test("first launch detects the relocated Claude account and saves it", async (terminal) => {
  const fixture = journey.current();
  await step("setup detects only the relocated installation", () => {}, async () => {
    await locate(terminal, "Initialize target harnesses").unique().expect();
    await locate(terminal, "[•] Claude Code").unique().expect();
    await locate(terminal, "> [ ] Factory Droid").unique().expect();
    await locate(terminal, /~ \d+ out of sync/).unique().expect({ timeout: 30_000 });
  });
  assert.equal(fs.existsSync(path.join(fixture.home, ".claude")), false);
  await step("save detected Claude Code", () => terminal.write("\r"), async () => {
    await locate(terminal, "Saved user harnesses").unique().expect();
    await locate(terminal, "Initialize target harnesses").expect({ not: true });
  });
  assert.equal(fs.readFileSync(path.join(fixture.home, ".agents/config.yaml"), "utf8"),
    "apiVersion: agent-marketplace/v4\nharnesses:\n  - claude\n");
  await step("quit with pending drift", () => terminal.write("q"), async () => {
    await locate(terminal, "pending changes not synced").expect();
  });
  await step("leave without syncing", () => terminal.write("n"), async () => {
    assert.equal((await waitForExit(terminal)).exitCode, 0);
  });
});

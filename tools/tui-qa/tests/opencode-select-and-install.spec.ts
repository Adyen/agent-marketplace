import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { after, before } from "node:test";
import { defineFixture, locate, step, waitForExit } from "./helpers.js";

const journey = defineFixture("opencode-select-and-install", ["tui"], 80, 24, { directorySource: true });
before(journey.setup);
after(journey.cleanup);

// spec: harness-opencode/opencode-selection-reaches-consumer-workflows
journey.test("selects OpenCode and installs a skill through the harness modal", async (terminal) => {
  const fixture = journey.current();
  await locate(terminal, "alpha-skill").expect();
  await locate(terminal, "~ in sync").unique().expect();
  await step("open harness choices", () => terminal.write("H"), async () => {
    await locate(terminal, "[ ] OpenCode").expect();
  });
  // Stable registry order puts the opt-in OpenCode choice after Codex.
  await terminal.write("\x1b[B".repeat(7));
  await step("enable OpenCode", () => terminal.write(" "), async () => {
    await locate(terminal, "[•] OpenCode").expect();
  });
  await step("save global selection", () => terminal.write("\r"), async () => {
    await locate(terminal, "Saved global harnesses").unique().expect();
  });
  assert.match(fs.readFileSync(path.join(fixture.home, ".agents", "config.yaml"), "utf8"), /harnesses:.*\bopencode\b/);
  await step("open search", () => terminal.write("/"), async () => {
    await locate(terminal, /\/\s{20,}/).unique().expect();
  });
  await step("filter alpha", () => terminal.write("alpha"), async () => {
    await locate(terminal, "alpha-skill").expect();
    await locate(terminal, "beta-skill").expect({ not: true });
  });
  await step("select alpha", () => terminal.write("\r"), async () => {
    await locate(terminal, /Name:\s+alpha-skill/).unique().expect();
  });
  await terminal.write(" ");
  await locate(terminal, "toggle global").unique().expect();
  await terminal.write("g");
  await locate(terminal, /Status:\s+enabled \(pending sync\)/).unique().expect();
  await step("install selected skill", () => terminal.write("s"), async () => {
    await locate(terminal, "Sync complete").unique().expect({ timeout: 30_000 });
  });
  const installed = path.join(fixture.env.XDG_CONFIG_HOME!, "opencode", "skills", "alpha-skill", "SKILL.md");
  assert.equal(fs.readFileSync(installed, "utf8"), fixture.expectedAlphaSkillContent);
  await terminal.write("q");
  assert.equal((await waitForExit(terminal)).exitCode, 0);
});

import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { expect, test } from "@microsoft/tui-test";
import { defineFixture, step, waitForExit } from "./helpers.js";

const journey = defineFixture("opencode-select-and-install", ["tui"], 80, 24, { directorySource: true });
test.use(journey.options);
test.beforeAll(journey.setup);
test.afterAll(journey.cleanup);

// spec: harness-opencode/opencode-selection-reaches-consumer-workflows
test("selects OpenCode and installs a skill through the harness modal", async ({ terminal }) => {
  const fixture = journey.current();
  await expect(terminal.getByText("alpha-skill", { strict: false })).toBeVisible();
  await step("open harness choices", () => terminal.write("H"), async () => {
    await expect(terminal.getByText("[ ] OpenCode", { strict: false })).toBeVisible();
  });
  // Stable registry order puts the opt-in OpenCode choice after Codex.
  await terminal.write("\x1b[B".repeat(7));
  await step("enable OpenCode", () => terminal.write(" "), async () => {
    await expect(terminal.getByText("[x] OpenCode", { strict: false })).toBeVisible();
  });
  await step("save global selection", () => terminal.write("\r"), async () => {
    await expect(terminal.getByText("Saved global harnesses")).toBeVisible();
  });
  assert.match(fs.readFileSync(path.join(fixture.home, ".agents", "config.yaml"), "utf8"), /harnesses:.*\bopencode\b/);
  await step("open search", () => terminal.write("/"), async () => {
    await expect(terminal.getByText(/\/\s{20,}/g)).toBeVisible();
  });
  await step("filter alpha", () => terminal.write("alpha"), async () => {
    await expect(terminal.getByText("alpha-skill", { strict: false })).toBeVisible();
    await expect(terminal.getByText("beta-skill", { strict: false })).not.toBeVisible();
  });
  await step("select alpha", () => terminal.write("\r"), async () => {
    await expect(terminal.getByText(/Name:\s+alpha-skill/g)).toBeVisible();
  });
  await terminal.write(" ");
  await expect(terminal.getByText("toggle global")).toBeVisible();
  await terminal.write("g");
  await expect(terminal.getByText(/Status:\s+enabled \(pending sync\)/g)).toBeVisible();
  await step("install selected skill", () => terminal.write("s"), async () => {
    await expect(terminal.getByText("Sync complete")).toBeVisible({ timeout: 30_000 });
  });
  const installed = path.join(fixture.env.XDG_CONFIG_HOME!, "opencode", "skills", "alpha-skill", "SKILL.md");
  assert.equal(fs.readFileSync(installed, "utf8"), fixture.expectedAlphaSkillContent);
  await terminal.write("q");
  assert.equal((await waitForExit(terminal)).exitCode, 0);
});

import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { after, before } from "node:test";

import { defineFixture, locate, step, waitForExit } from "./helpers.js";
import { write } from "./process-helpers.js";

const journey = defineFixture("materialization-warning", ["tui"], 80, 24, { directorySource: true });
before(async () => {
  await journey.setup();
  const fixture = journey.current();
  write(path.join(fixture.registryURL, ".factory", "skills", "restricted", "SKILL.md"), "---\nname: restricted\ndescription: Tool restriction fixture\nallowed-tools: [Read]\n---\nReview fixture.\n");
  write(path.join(fixture.home, ".agents", "config.yaml"), `apiVersion: agent-marketplace/v4\nharnesses: [claude]\nsources:\n  local:\n    directory: ${fixture.registryURL}\n    source_harness: factory\n`);
});
after(journey.cleanup);

// spec: hook-sync-diagnostics/mutation-commands-retain-materialization-warnings
// spec: hook-sync-diagnostics/sync-and-quit-presents-new-hook-review
journey.test("sync and quit retains a compiler warning without changing success", async (terminal) => {
  const fixture = journey.current();
  await step("settled restricted skill", () => {}, async () => {
    await locate(terminal, "restricted").expect();
    await locate(terminal, "~ in sync").expect();
  });
  await step("select restricted skill", () => terminal.write("G"), async () => {
    await locate(terminal, /Name:\s+restricted/).expect();
  });
  await step("open scope", () => terminal.write(" "), async () => {
    await locate(terminal, "toggle project").expect();
  });
  await step("enable skill", () => terminal.write("p"), async () => {
    await locate(terminal, "out of sync").expect();
  });
  await step("request sync and quit", () => terminal.write("q"), async () => {
    await locate(terminal, "Sync+quit?").expect();
  });
  await step("review compiler warning before exit", () => terminal.write("y"), async () => {
    await locate(terminal, "skill.allowed-tools-omitted").expect({ timeout: 30_000 });
    await locate(terminal, "Warnings (1)").expect();
  });
  const installed = fs.readFileSync(path.join(fixture.repo, ".claude", "skills", "restricted", "SKILL.md"), "utf8");
  assert.match(installed, /Review fixture\./);
  assert.doesNotMatch(installed, /allowed-tools/);
  await step("close warning", () => terminal.write("\x1b"), async () => {
    await locate(terminal, "~ in sync").expect();
  });
  await step("quit successfully", () => terminal.write("q"), async () => {
    assert.equal((await waitForExit(terminal)).exitCode, 0);
  });
});

import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { after, before } from "node:test";

import { defineFixture, locate, step, type TestTerminal, waitForExit } from "./helpers.js";

// spec: plugin-packages/toggling-a-member-row-edits-exclusion
const journey = defineFixture("plugin-member-exclusion", ["tui"], 80, 24, { pluginSource: true });
before(journey.setup);
after(journey.cleanup);

async function selectReviewMember(terminal: TestTerminal): Promise<void> {
  await step("select the final nested plugin member", () => terminal.write("G\r"), async () => {
    await locate(terminal, /Review member\./).first().expect();
  });
  await step("return to the member row", () => terminal.write("\x1b"), async () => {
    await locate(terminal, "skill t/review").expect();
  });
}

journey.test("member toggles edit only the selected plugin exclusion", async (terminal) => {
  const fixture = journey.current();
  const configPath = path.join(fixture.repo, ".agents", "config.yaml");

  await locate(terminal, "plugin t/kit").expect();
  await locate(terminal, "skill t/review").expect();
  await selectReviewMember(terminal);
  await step("open member toggle options", () => terminal.write(" "), async () => {
    await locate(terminal, "toggle project").expect();
  });
  await step("exclude member from selected plugin", () => terminal.write("p"), async () => {
    await locate(terminal, /○.*skill t\/review/).first().expect();
  });
  let config = fs.readFileSync(configPath, "utf8");
  assert.match(config, /include:\n\s+- kind: plugin\n\s+name: kit/);
  assert.match(config, /exclude:\n\s+- skill\/review/);
  assert.doesNotMatch(config, /^      - skill\/review$/m);

  await step("confirm excluded plugin member state", () => terminal.write("G"), async () => {
    await locate(terminal, /Status:\s+excluded\b/).first().expect();
  });

  await selectReviewMember(terminal);
  await step("reopen member toggle options", () => terminal.write(" "), async () => {
    await locate(terminal, "toggle project").expect();
  });
  await step("restore member in selected plugin", () => terminal.write("p"), async () => {
    await locate(terminal, /●.*skill t\/review/).first().expect();
    await locate(terminal, /Status:\s+enabled/).first().expect();
    await locate(terminal, /Status:\s+enabled \(pending sync\)/).expect({ not: true });
    await locate(terminal, /Pending sync\s+\(\d+\)/).expect({ not: true });
  });
  config = fs.readFileSync(configPath, "utf8");
  assert.match(config, /include:\n\s+- kind: plugin\n\s+name: kit/);
  assert.doesNotMatch(config, /exclude:[\s\S]*?skill\/review/);
  assert.doesNotMatch(config, /^      - skill\/review$/m);

  await step("quit with Ctrl+C", () => terminal.write("\u0003"), async () => {
    assert.equal((await waitForExit(terminal)).exitCode, 0);
  });
});

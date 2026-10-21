import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";

import { expect, test } from "@microsoft/tui-test";

import { defineFixture, step, type TestTerminal, waitForExit } from "./helpers.js";

// spec: plugin-packages/toggling-a-member-row-edits-exclusion
const journey = defineFixture("plugin-member-exclusion", ["tui"], 80, 24, { pluginSource: true });
test.use(journey.options);
test.beforeAll(journey.setup);
test.afterAll(journey.cleanup);

async function selectReviewMember(terminal: TestTerminal): Promise<void> {
  await step("select the final nested plugin member", () => terminal.write("G\r"), async () => {
    await expect(terminal.getByText(/Review member\./g)).toBeVisible();
  });
  await step("return to the member row", () => terminal.write("\x1b"), async () => {
    await expect(terminal.getByText("skill t/review", { strict: false })).toBeVisible();
  });
}

test("member toggles edit only the selected plugin exclusion", async ({ terminal }) => {
  const fixture = journey.current();
  const configPath = path.join(fixture.repo, ".agents", "config.yaml");

  await expect(terminal.getByText("plugin t/kit", { strict: false })).toBeVisible();
  await expect(terminal.getByText("skill t/review", { strict: false })).toBeVisible();
  await selectReviewMember(terminal);
  await step("open member toggle options", () => terminal.write(" "), async () => {
    await expect(terminal.getByText("toggle project")).toBeVisible();
  });
  await step("exclude member from selected plugin", () => terminal.write("p"), async () => {
    await expect(terminal.getByText(/○.*skill t\/review/g)).toBeVisible();
  });
  let config = fs.readFileSync(configPath, "utf8");
  assert.match(config, /include:\n\s+- kind: plugin\n\s+name: kit/);
  assert.match(config, /exclude:\n\s+- skill\/review/);
  assert.doesNotMatch(config, /^      - skill\/review$/m);

  await step("confirm excluded plugin member state", () => terminal.write("G"), async () => {
    await expect(terminal.getByText(/Status:\s+excluded\b/g)).toBeVisible();
  });

  await selectReviewMember(terminal);
  await step("reopen member toggle options", () => terminal.write(" "), async () => {
    await expect(terminal.getByText("toggle project")).toBeVisible();
  });
  await step("restore member in selected plugin", () => terminal.write("p"), async () => {
    await expect(terminal.getByText(/●.*skill t\/review/g)).toBeVisible();
    await expect(terminal.getByText(/Status:\s+enabled(?! \(pending sync\))/g)).toBeVisible();
    await expect(terminal.getByText(/Pending sync\s+\(\d+\)/g)).not.toBeVisible();
  });
  config = fs.readFileSync(configPath, "utf8");
  assert.match(config, /include:\n\s+- kind: plugin\n\s+name: kit/);
  assert.doesNotMatch(config, /exclude:[\s\S]*?skill\/review/);
  assert.doesNotMatch(config, /^      - skill\/review$/m);

  await step("quit with Ctrl+C", () => terminal.write("\u0003"), async () => {
    assert.equal((await waitForExit(terminal)).exitCode, 0);
  });
});

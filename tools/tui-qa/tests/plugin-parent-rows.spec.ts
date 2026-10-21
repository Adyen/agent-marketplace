import assert from "node:assert/strict";

import { expect, test } from "@microsoft/tui-test";

import { defineFixture, findTextRun } from "./helpers.js";

// spec: plugin-packages/plugin-members-appear-under-their-plugin
const journey = defineFixture("plugin-parent-rows", ["tui"], 80, 24, { pluginSource: true });
test.use(journey.options);
test.beforeAll(journey.setup);
test.afterAll(journey.cleanup);

test("plugin members render as children of one parent row", async ({ terminal }) => {
  await expect(terminal.getByText("plugin t/kit", { strict: false })).toBeVisible();
  await expect(terminal.getByText("skill t/review", { strict: false })).toBeVisible();
  await expect(terminal.getByText(/hook t\//g, { strict: false })).toBeVisible();
  await expect(terminal.getByText("mcp t/idx", { strict: false })).toBeVisible();

  const parent = findTextRun(terminal, /plugin t\/kit/);
  const skill = findTextRun(terminal, /skill t\/review/);
  const hook = findTextRun(terminal, /hook t\/Stop-command/);
  const mcp = findTextRun(terminal, /mcp t\/idx/);
  assert.ok(parent && skill && hook && mcp, "plugin parent and all member rows must be on screen");
});

import assert from "node:assert/strict";
import { after, before } from "node:test";

import { defineFixture, findTextRun, locate } from "./helpers.js";

// spec: plugin-packages/plugin-members-appear-under-their-plugin
const journey = defineFixture("plugin-parent-rows", ["tui"], 80, 24, { pluginSource: true });
before(journey.setup);
after(journey.cleanup);

journey.test("plugin members render as children of one parent row", async (terminal) => {
  await locate(terminal, "plugin t/kit").expect();
  await locate(terminal, "skill t/review").expect();
  await locate(terminal, /hook t\//).first().expect();
  await locate(terminal, "mcp t/idx").expect();

  const parent = await findTextRun(terminal, /plugin t\/kit/);
  const skill = await findTextRun(terminal, /skill t\/review/);
  const hook = await findTextRun(terminal, /hook t\/Stop-command/);
  const mcp = await findTextRun(terminal, /mcp t\/idx/);
  assert.ok(parent && skill && hook && mcp, "plugin parent and all member rows must be on screen");
});

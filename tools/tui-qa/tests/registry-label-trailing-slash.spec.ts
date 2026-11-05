import assert from "node:assert/strict";
import { after, before } from "node:test";

import { defineFixture, locate, step, waitForExit } from "./helpers.js";
import { runNonTTY } from "./process-helpers.js";

// spec: tui-rendering/aligned-safe-labels-at-normal-and-narrow-sizes
const journey = defineFixture("registry-label-trailing-slash", ["tui"], 80, 24, { trailingSlashRegistry: true });
before(journey.setup);
after(journey.cleanup);

journey.test("a registry URL with a trailing slash still labels its rows with the registry name", async (terminal) => {
  assert.ok(journey.current().registryURL.endsWith(".git/"), "the fixture registry URL must end with a slash");
  await step("settled fixture baseline", () => {}, async () => {
    await locate(terminal, "~ in sync").unique().expect({ timeout: 30_000 });
  });
  await step("rows name the fixture registry, not (local)", () => {}, async () => {
    await locate(terminal, "fixture/beta-skill").unique().expect();
    await locate(terminal, "fixture/alpha-skill").unique().expect();
    await locate(terminal, "(local)/").expect({ not: true });
  });
  await step("quit", () => terminal.write("q"), async () => {
    assert.equal((await waitForExit(terminal)).exitCode, 0);
  });
  const check = runNonTTY(journey.current(), ["sync", "--check"], 30_000);
  if (check.error) throw check.error;
  assert.equal(check.status, 0, `${check.stdout}\n${check.stderr}`);
});

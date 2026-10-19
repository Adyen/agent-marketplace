import assert from "node:assert/strict";

import { expect, test } from "@microsoft/tui-test";

import { defineFixture, step, waitForExit } from "./helpers.js";
import { runAM } from "./process-helpers.js";

// The deployed-lock counterpart to the transient-acquisition journey. The TUI
// must honor the same exact organization resource from the published policy
// lock and reach its settled baseline without changing behavior.
const journey = defineFixture("org-marketplace-lock-recovery", ["tui"], 80, 24, { organization: "published-lock" });
test.use(journey.options);
test.beforeAll(journey.setup);
test.afterAll(journey.cleanup);

test("a published organization lock keeps the marketplace-backed workspace healthy", async ({ terminal }) => {
  const fixture = journey.current();

  await step("settled baseline with the organization marketplace", () => {}, async () => {
    await expect(terminal.getByText("beta-skill", { strict: false })).toBeVisible();
    await expect(terminal.getByText("alpha-skill", { strict: false })).toBeVisible();
    await expect(terminal.getByText("org-review", { strict: false })).toBeVisible();
    await expect(terminal.getByText("~ in sync")).toBeVisible();
  });

  await step("no error or marketplace failure markers", () => {}, async () => {
    await expect(terminal.getByText(/! \d+ errors/g)).not.toBeVisible();
    await expect(terminal.getByText(/~ drift unavailable/g)).not.toBeVisible();
    await expect(terminal.getByText(/Marketplace: \d+ (stale|limited|failed)/g)).not.toBeVisible();
  });

  await step("status groups render in the single view", () => terminal.write("3"), async () => {
    await expect(terminal.getByText(/Required\s+\(1\)/g)).toBeVisible();
    await expect(terminal.getByText(/Enabled\s+\(1\)/g)).toBeVisible();
    await expect(terminal.getByText(/Available\s+\(1\)/g)).toBeVisible();
    await expect(terminal.getByText(/1 Scope\s+2 Registry\s+3 Status/g)).not.toBeVisible();
  });

  await step("quit", () => terminal.write("q"), async () => {
    assert.equal((await waitForExit(terminal)).exitCode, 0);
  });

  // The drift check talks to the organization marketplace stub, so it uses the
  // event-loop-free runner.
  const check = await runAM(fixture, ["sync", "--check"]);
  assert.equal(check.status, 0, `${check.stdout}\n${check.stderr}`);
});

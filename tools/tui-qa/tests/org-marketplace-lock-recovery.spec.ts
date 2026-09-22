import assert from "node:assert/strict";

import { expect, test } from "@microsoft/tui-test";

import { declineLegacyMigration, defineFixture, step, waitForExit } from "./helpers.js";
import { runAM } from "./process-helpers.js";

// The healthy counterpart to the lock-failure journey: the same organization
// v4 Marketplace declaration with its acquisition lock published. The TUI must
// reach its settled baseline with the endpoint live, and drift checks must
// pass, proving the organization layer is the only difference.
const journey = defineFixture("org-marketplace-lock-recovery", ["tui"], 80, 24, { organization: "published-lock" });
test.use(journey.options);
test.beforeAll(journey.setup);
test.afterAll(journey.cleanup);

test("a published organization lock keeps the marketplace-backed workspace healthy", async ({ terminal }) => {
  const fixture = journey.current();
  await declineLegacyMigration(terminal);

  await step("settled baseline with the organization marketplace", () => {}, async () => {
    await expect(terminal.getByText("beta-skill", { strict: false })).toBeVisible();
    await expect(terminal.getByText("alpha-skill", { strict: false })).toBeVisible();
    await expect(terminal.getByText("~ in sync")).toBeVisible();
  });

  await step("no error or marketplace failure markers", () => {}, async () => {
    await expect(terminal.getByText(/! \d+ errors/g)).not.toBeVisible();
    await expect(terminal.getByText(/~ drift unavailable/g)).not.toBeVisible();
    await expect(terminal.getByText(/Marketplace: \d+ (stale|limited|failed)/g)).not.toBeVisible();
  });

  await step("status tab groups render", () => terminal.write("3"), async () => {
    await expect(terminal.getByText(/Enabled\s+\(1\)/g)).toBeVisible();
    await expect(terminal.getByText(/Available\s+\(1\)/g)).toBeVisible();
  });

  await step("return to scope tab", () => terminal.write("1"), async () => {
    await expect(terminal.getByText(/Project\s+\(1\)/g)).toBeVisible();
  });

  await step("quit", () => terminal.write("q"), async () => {
    assert.equal((await waitForExit(terminal)).exitCode, 0);
  });

  // The drift check talks to the organization marketplace stub, so it uses the
  // event-loop-free runner.
  const check = await runAM(fixture, ["sync", "--check"]);
  assert.equal(check.status, 0, `${check.stdout}\n${check.stderr}`);
});

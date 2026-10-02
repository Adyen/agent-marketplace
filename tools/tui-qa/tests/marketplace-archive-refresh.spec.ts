import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";

import { expect, test } from "@microsoft/tui-test";

import { defineFixture, step, waitForExit } from "./helpers.js";

const journey = defineFixture("marketplace-archive-refresh", ["tui"], 80, 24, { userMarketplaceDrift: true });
test.use(journey.options);
test.beforeAll(journey.setup);
test.afterAll(journey.cleanup);

test("update refreshes drifted marketplace archive evidence with a visible warning", async ({ terminal }) => {
  const fixture = journey.current();
  assert.ok(fixture.userLockPath, "fixture did not expose the user lock path");
  assert.ok(fixture.marketplaceDriftedArchiveDigest, "fixture did not expose the drifted archive digest");
  await step("settled baseline with the pinned archive evidence", () => {}, async () => {
    await expect(terminal.getByText("org-review", { strict: false })).toBeVisible();
    await expect(terminal.getByText(/! \d+ errors/g)).not.toBeVisible();
    await expect(terminal.getByText(/⚠ \d+ warnings/g)).not.toBeVisible();
  });
  const lockedBefore = fs.readFileSync(fixture.userLockPath, "utf8");
  assert.ok(!lockedBefore.includes(fixture.marketplaceDriftedArchiveDigest), `user lock already carried the drifted digest:\n${lockedBefore}`);

  await step("update refreshes and reports the drifted archive evidence", () => terminal.write("u"), async () => {
    await expect(terminal.getByText(/Update complete/g)).toBeVisible();
    await expect(terminal.getByText("⚠ 1 warnings", { strict: false })).toBeVisible();
  });

  await step("warning names the refresh diagnostic", () => terminal.write("e"), async () => {
    await expect(terminal.getByText("Warnings (1)", { strict: false })).toBeVisible();
    await expect(terminal.getByText("marketplace.archive-evidence-refreshed", { strict: false })).toBeVisible();
  });

  await step("close diagnostics", () => terminal.write("e"), async () => {
    await expect(terminal.getByText("Warnings (1)", { strict: false })).not.toBeVisible();
  });

  const lockedAfter = fs.readFileSync(fixture.userLockPath, "utf8");
  assert.ok(lockedAfter.includes(fixture.marketplaceDriftedArchiveDigest), `user lock kept the stale archive digest:\n${lockedAfter}`);
  const materialized = path.join(fixture.home, ".factory", "skills", "org-review", "SKILL.md");
  assert.equal(fs.readFileSync(materialized, "utf8"), fixture.expectedOrgSkillContent);

  await step("healthy repeat update stays quiet", () => terminal.write("u"), async () => {
    await expect(terminal.getByText("⚠ 1 warnings", { strict: false })).not.toBeVisible();
  });
  assert.equal(fs.readFileSync(fixture.userLockPath, "utf8"), lockedAfter, "healthy update rewrote the lock");

  await step("quit", () => terminal.write("q"), async () => {
    assert.equal((await waitForExit(terminal)).exitCode, 0);
  });
});

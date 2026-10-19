import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { after, before } from "node:test";

import { assertPlaceholderArchivesFetched, defineFixture, locate, step, waitForExit } from "./helpers.js";

// spec: marketplace-api/tui-sync-and-update-report-the-refresh-non-fatally
// spec: marketplace-api/client-verifies-placeholder-archives-by-hash
const journey = defineFixture("marketplace-archive-refresh", ["tui"], 80, 24, { userMarketplaceDrift: true });
before(journey.setup);
after(journey.cleanup);

journey.test("update refreshes drifted marketplace archive evidence with a visible warning", async (terminal) => {
  const fixture = journey.current();
  assert.ok(fixture.userLockPath, "fixture did not expose the user lock path");
  assert.ok(fixture.marketplaceDriftedArchiveDigest, "fixture did not expose the drifted archive digest");
  await step("settled baseline with the pinned archive evidence", () => {}, async () => {
    await locate(terminal, "org-review").expect();
    await locate(terminal, "~ in sync").unique().expect();
    await locate(terminal, /! \d+ errors/).expect({ not: true });
    await locate(terminal, /⚠ \d+ warnings/).expect({ not: true });
  });
  const lockedBefore = fs.readFileSync(fixture.userLockPath, "utf8");
  assert.ok(!lockedBefore.includes(fixture.marketplaceDriftedArchiveDigest), `user lock already carried the drifted digest:\n${lockedBefore}`);

  await step("update refreshes and reports the drifted archive evidence", () => terminal.write("u"), async () => {
    await locate(terminal, /Update complete/).unique().expect();
    await locate(terminal, "⚠ 1 warnings").expect();
  });

  await step("warning names the refresh diagnostic", () => terminal.write("e"), async () => {
    await locate(terminal, "Warnings (1)").expect();
    await locate(terminal, "marketplace.archive-evidence-refreshed").expect();
  });

  await step("close diagnostics", () => terminal.write("e"), async () => {
    await locate(terminal, "Warnings (1)").expect({ not: true });
  });

  const lockedAfter = fs.readFileSync(fixture.userLockPath, "utf8");
  assert.ok(lockedAfter.includes(fixture.marketplaceDriftedArchiveDigest), `user lock kept the stale archive digest:\n${lockedAfter}`);
  const materialized = path.join(fixture.home, ".factory", "skills", "org-review", "SKILL.md");
  assert.equal(fs.readFileSync(materialized, "utf8"), fixture.expectedOrgSkillContent);
  assertPlaceholderArchivesFetched(fixture, fixture.marketplaceArchiveDigest, fixture.marketplaceDriftedArchiveDigest);

  await step("healthy repeat update stays quiet", () => terminal.write("u"), async () => {
    await locate(terminal, "⚠ 1 warnings").expect({ not: true });
  });
  assert.equal(fs.readFileSync(fixture.userLockPath, "utf8"), lockedAfter, "healthy update rewrote the lock");

  await step("quit", () => terminal.write("q"), async () => {
    assert.equal((await waitForExit(terminal)).exitCode, 0);
  });
});

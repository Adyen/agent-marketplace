import assert from "node:assert/strict";
import { after, before } from "node:test";

import { assertArchivesVerifiedByReceivedBytes, defineFixture, locate, step, waitForExit } from "./helpers.js";
import { runAM } from "./process-helpers.js";

// The deployed-lock counterpart to the transient-acquisition journey. The TUI
// must honor the same exact organization resource from the published policy
// lock and reach its settled baseline without changing behavior. The lock pins
// an archive digest no Go toolchain reproduces, so it stays valid only through
// verification of the received and cached placeholder bytes.
// spec: marketplace-api/locks-written-under-any-toolchain-stay-valid
const journey = defineFixture("org-marketplace-lock-recovery", ["tui"], 80, 24, { organization: "published-lock" });
before(journey.setup);
after(journey.cleanup);

journey.test("a published organization lock keeps the marketplace-backed workspace healthy", async (terminal) => {
  const fixture = journey.current();

  await step("settled baseline with the organization marketplace", () => {}, async () => {
    await locate(terminal, "beta-skill").expect();
    await locate(terminal, "alpha-skill").expect();
    await locate(terminal, "org-review").expect();
    await locate(terminal, "~ in sync").unique().expect();
  });

  await step("no error or marketplace failure markers", () => {}, async () => {
    await locate(terminal, /! \d+ errors/).expect({ not: true });
    await locate(terminal, /~ drift unavailable/).expect({ not: true });
    await locate(terminal, /Marketplace: \d+ (stale|limited|failed)/).expect({ not: true });
  });

  await step("status groups render in the single view", () => terminal.write("3"), async () => {
    await locate(terminal, /Required\s+\(1\)/).unique().expect();
    await locate(terminal, /Enabled\s+\(1\)/).unique().expect();
    await locate(terminal, /Available\s+\(1\)/).unique().expect();
    await locate(terminal, /1 Scope\s+2 Registry\s+3 Status/).expect({ not: true });
  });

  await step("quit", () => terminal.write("q"), async () => {
    assert.equal((await waitForExit(terminal)).exitCode, 0);
  });

  // The drift check talks to the organization marketplace stub, so it uses the
  // event-loop-free runner.
  const check = await runAM(fixture, ["sync", "--check"]);
  assert.equal(check.status, 0, `${check.stdout}\n${check.stderr}`);
  assertArchivesVerifiedByReceivedBytes(fixture, fixture.marketplaceArchiveDigest);
});

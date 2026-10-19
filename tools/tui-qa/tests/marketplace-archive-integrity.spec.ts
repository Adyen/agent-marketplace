import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { after, before } from "node:test";

import { defineFixture, locate, step, waitForExit } from "./helpers.js";

// The counterpart to the journeys that fetch placeholder archives: the stub
// serves a valid placeholder archive with the right content whose bytes do not
// hash to the archive digest. Only a client that hashes the bytes it receives
// rejects it, so the cold TUI acquisition must fail closed: nothing is
// installed, no organization lock is written, and the client does not retry
// through the legacy final-artifact route.
// spec: marketplace-api/client-verifies-placeholder-archives-by-hash
const journey = defineFixture("marketplace-archive-integrity", ["tui"], 80, 24, {
  organization: "missing-lock",
  mismatchedPlaceholderArchive: true
});
before(journey.setup);
after(journey.cleanup);

journey.test("placeholder archive bytes that do not hash to the archive digest are rejected", async (terminal) => {
  const fixture = journey.current();
  assert.ok(fixture.orgLockPath, "organization fixture must expose its lock path");
  const orgSkill = path.join(fixture.home, ".factory", "skills", "org-review");

  await step("cold startup reports the rejected acquisition", () => {}, async () => {
    await locate(terminal, /! 1 errors/).unique().expect({ timeout: 30_000 });
  });

  await step("the diagnostic names the archive that failed verification", () => terminal.write("e"), async () => {
    await locate(terminal, "Errors (1)").unique().expect();
    await locate(terminal, "validate artifact archive").expect();
    await locate(terminal, "does not match requested").expect();
  });

  await step("close diagnostics", () => terminal.write("e"), async () => {
    await locate(terminal, "Errors (1)").expect({ not: true });
  });

  await step("quit", () => terminal.write("q"), async () => {
    assert.equal((await waitForExit(terminal)).exitCode, 0);
  });

  assert.equal(fs.lstatSync(orgSkill, { throwIfNoEntry: false }), undefined, "the rejected archive was installed");
  assert.equal(fs.lstatSync(fixture.orgLockPath, { throwIfNoEntry: false }), undefined, "the rejected acquisition wrote an organization lock");
  const fetches = fixture.marketplaceArchiveFetches?.();
  assert.ok(fetches, "fixture has no Marketplace stub");
  assert.ok(
    fixture.marketplaceArchiveDigest && fetches.placeholder.includes(fixture.marketplaceArchiveDigest),
    `the client never received the mismatched placeholder archive: ${JSON.stringify(fetches)}`
  );
  assert.deepEqual(fetches.final, [], "the client retried through the legacy final-artifact route");
});

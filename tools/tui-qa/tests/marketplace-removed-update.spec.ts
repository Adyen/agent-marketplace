import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { after, before } from "node:test";

import { defineFixture, locate, step, waitForExit } from "./helpers.js";

// spec: tui-error-recovery/tui-update-shows-the-removed-resource-warning
// spec: tui-error-recovery/tui-update-fails-when-the-pinned-revision-is-unusable
const journey = defineFixture("marketplace-removed-update", ["tui"], 80, 24, { userMarketplaceRemoved: true });
before(journey.setup);
after(journey.cleanup);

journey.test("update warns for a kept Marketplace pin and fails when its revision is unavailable", async (terminal) => {
  const fixture = journey.current();
  assert.ok(fixture.userLockPath, "fixture did not expose its Marketplace lock");
  await step("settled Marketplace baseline", () => {}, async () => {
    await locate(terminal, "org-review").expect();
    await locate(terminal, "~ in sync").unique().expect({ timeout: 30_000 });
  });
  const lockBefore = fs.readFileSync(fixture.userLockPath);
  fixture.removeMarketplaceLatest?.();

  await step("update completes with the removed-resource warning", () => terminal.write("u"), async () => {
    await locate(terminal, "Update complete with 1 warning(s)").unique().expect({ timeout: 30_000 });
    await locate(terminal, "⚠ 1 warnings").unique().expect();
  });
  await step("diagnostics name the kept pin and its removal remedy", () => terminal.write("e"), async () => {
    await locate(terminal, "Warnings (1)").unique().expect();
    const screen = (await terminal.text()).replace(/\s+/g, "");
    assert.ok(screen.includes("/skill/org-review"), `diagnostics omit the Marketplace coordinate:\n${await terminal.text()}`);
    assert.ok(screen.includes("agent-marketplaceremoveorg-review"), `diagnostics omit the removal remedy:\n${await terminal.text()}`);
  });
  assert.deepEqual(fs.readFileSync(fixture.userLockPath), lockBefore, "kept update changed the acquisition lock");
  await step("close warning diagnostics", () => terminal.write("e"), async () => {
    await locate(terminal, "Warnings (1)").expect({ not: true });
  });

  fixture.removeMarketplaceExactRevision?.();
  fs.rmSync(path.join(fixture.cacheDir, "marketplace"), { recursive: true, force: true });
  await step("update fails when neither the endpoint nor verified cache has the pin", () => terminal.write("u"), async () => {
    await locate(terminal, /! \d+ errors: update/).unique().expect({ timeout: 30_000 });
  });
  await step("errors name the pin and the remove-and-install remedy", () => terminal.write("e"), async () => {
    await locate(terminal, /Errors \(\d+\)/).unique().expect();
    const screen = (await terminal.text()).replace(/\s+/g, "");
    assert.ok(screen.includes("/skill/org-review"), `errors omit the Marketplace coordinate:\n${await terminal.text()}`);
    assert.ok(screen.includes("agent-marketplaceremoveorg-review"), `errors omit the removal remedy:\n${await terminal.text()}`);
    assert.ok(screen.includes("installthenewname"), `errors omit the install remedy:\n${await terminal.text()}`);
  });
  assert.deepEqual(fs.readFileSync(fixture.userLockPath), lockBefore, "failed update changed the acquisition lock");

  await step("close error diagnostics", () => terminal.write("\u001b"), async () => {
    await locate(terminal, /Errors \(\d+\)/).expect({ not: true });
  });
  await step("open the unsynced exit prompt", () => terminal.write("q"), async () => {
    await locate(terminal, "Confirm").unique().expect();
  });
  await step("quit without syncing", () => terminal.write("n\r"), async () => {
    assert.equal((await waitForExit(terminal)).exitCode, 0);
  });
});

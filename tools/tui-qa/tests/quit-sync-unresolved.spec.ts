import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { after, before } from "node:test";

import { defineFixture, locate, step, waitForExit, waitForSignal } from "./helpers.js";

let acquisitionGateArmed = false;
let releaseAcquisition: () => void = () => {};
let signalAcquisitionHeld: () => void = () => {};
const acquisitionRelease = new Promise<void>((resolve) => { releaseAcquisition = resolve; });
const acquisitionHeld = new Promise<void>((resolve) => { signalAcquisitionHeld = resolve; });
const acquisitionGate = async (): Promise<void> => {
  if (!acquisitionGateArmed) return;
  acquisitionGateArmed = false;
  signalAcquisitionHeld();
  await acquisitionRelease;
};

// spec: tui-feedback/remaining-drift-after-sync-keeps-tui-open-with-diagnostics
const journey = defineFixture("quit-sync-unresolved", ["tui"], 80, 24, {
  userMarketplaceDrift: true,
  marketplaceAcquisitionGate: acquisitionGate
});
before(journey.setup);
after(journey.cleanup);

journey.test("sync+quit stays open when the authoritative snapshot finds new pending selections", async (terminal) => {
  const fixture = journey.current();
  const repoConfig = path.join(fixture.repo, ".agents", "config.yaml");
  const betaSkill = path.join(fixture.repo, ".factory", "skills", "beta-skill");
  const alphaSkill = path.join(fixture.repo, ".factory", "skills", "alpha-skill");
  const withoutBeta = fixture.expectedRepoConfig.replace(
    "    include:\n      - skill/beta-skill\n",
    "    include: []\n"
  );
  const changedDuringSync = fixture.expectedRepoConfig.replace(
    "      - skill/beta-skill\n",
    "      - skill/beta-skill\n      - skill/alpha-skill\n"
  );

  await step("settled fixture baseline", () => {}, async () => {
    await locate(terminal, "beta-skill").expect();
    await locate(terminal, "~ in sync").unique().expect({ timeout: 30_000 });
  });

  fs.writeFileSync(repoConfig, withoutBeta, { encoding: "utf8", mode: 0o600 });
  await step("reload creates one pending removal", () => terminal.write("r"), async () => {
    await locate(terminal, "~ 1 out of sync").unique().expect({ timeout: 30_000 });
  });
  await step("quit confirmation identifies the pending removal", () => terminal.write("q"), async () => {
    await locate(terminal, "1 pending changes not synced. Sync+quit?").unique().expect();
    await locate(terminal, "- remove beta-skill").unique().expect();
  });

  assert.ok(fixture.userLockPath, "fixture did not expose the Marketplace lock");
  fs.rmSync(fixture.userLockPath);
  acquisitionGateArmed = true;
  await step("sync waits at the Marketplace acquisition boundary", () => terminal.write("y"), async () => {
    await waitForSignal(acquisitionHeld, "sync Marketplace acquisition");
  });
  let markBetaRemoved: () => void = () => {};
  const betaRemoved = new Promise<void>((resolve) => { markBetaRemoved = resolve; });
  const watcher = fs.watch(path.dirname(betaSkill), (_event, filename) => {
    if (filename?.toString() === path.basename(betaSkill) && !fs.existsSync(betaSkill)) markBetaRemoved();
  });
  releaseAcquisition();
  try {
    await waitForSignal(betaRemoved, "sync removal of beta-skill");
  } finally {
    watcher.close();
  }
  assert.equal(fs.lstatSync(betaSkill, { throwIfNoEntry: false }), undefined, "sync did not remove the deselected beta-skill");
  fs.writeFileSync(repoConfig, changedDuringSync, { encoding: "utf8", mode: 0o600 });

  await step("authoritative reload keeps the unresolved state visible", () => {}, async () => {
    await locate(terminal, "~ 2 out of sync").unique().expect({ timeout: 30_000 });
    await locate(terminal, /⚠ \d+ warnings/).unique().expect();
  });
  await step("diagnostics explain that drift remains", () => terminal.write("e"), async () => {
    await locate(terminal, /Warnings \(\d+\)/).unique().expect();
    await locate(terminal, "sync complete with 2 changes still pending").unique().expect();
  });
  await step("close diagnostics and inspect pending installs", () => terminal.write("e"), async () => {
    await locate(terminal, /Warnings \(\d+\)/).expect({ not: true });
    await locate(terminal, "~ 2 out of sync").unique().expect();
  });
  await step("the next quit lists unresolved resources", () => terminal.write("q"), async () => {
    await locate(terminal, "2 pending changes not synced. Sync+quit?").unique().expect();
    await locate(terminal, "+ install skill/alpha-skill").unique().expect();
    await locate(terminal, "+ install skill/beta-skill").unique().expect();
  });
  await step("choosing quit exits without another sync", () => terminal.write("n"), async () => {
    assert.equal((await waitForExit(terminal)).exitCode, 0);
  });

  assert.equal(fs.lstatSync(alphaSkill, { throwIfNoEntry: false }), undefined, "sync installed a selection added after the sync snapshot");
  assert.equal(fs.lstatSync(betaSkill, { throwIfNoEntry: false }), undefined, "the original sync did not remove the deselected beta-skill");
});

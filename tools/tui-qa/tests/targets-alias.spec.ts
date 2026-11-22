import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { after, before } from "node:test";

import { defineFixture, locate, step, waitForExit, type TestTerminal } from "./helpers.js";
import type { Fixture } from "./fixture.js";

const configureGlobalFixture = (fixture: Fixture, userConfig: string, organizationHarnesses = "factory, claude"): void => {
  if (!fixture.orgConfigPath) throw new Error("organization fixture has no config path");
  fs.writeFileSync(fixture.orgConfigPath, `apiVersion: agent-marketplace/v4\nharnesses: [${organizationHarnesses}]\nsources: {}\n`);
  if (fixture.orgLockPath) fs.rmSync(fixture.orgLockPath, { force: true });
  fs.writeFileSync(path.join(fixture.home, ".agents", "config.yaml"), userConfig);
};

const quitTui = async (terminal: TestTerminal): Promise<void> => {
  await step("show quit confirmation", () => terminal.write("q"), async () => {
    await locate(terminal, "Confirm").unique().expect();
  });
  await step("quit without syncing", () => terminal.write("n"), async () => {
    assert.equal((await waitForExit(terminal)).exitCode, 0);
  });
};

const refuseEmptyGlobalSelection = async (terminal: TestTerminal, fixture: Fixture): Promise<void> => {
  const userConfig = path.join(fixture.home, ".agents", "config.yaml");
  const userLock = path.join(fixture.home, ".agents", "lock.yaml");

  await step("settled fixture baseline", () => {}, async () => {
    await locate(terminal, /Pending sync\s+\(1\)/).unique().expect({ timeout: 30_000 });
  });
  await step("open the global harness editor", () => terminal.write("H"), async () => {
    await locate(terminal, /Editing:\s+global/).unique().expect();
    await locate(terminal, "> [•] Factory Droid").unique().expect();
  });
  const configBefore = fs.readFileSync(userConfig);
  const lockBefore = fs.existsSync(userLock) ? fs.readFileSync(userLock) : null;
  await step("deselect the last target", () => terminal.write(" "), async () => {
    await locate(terminal, "> [ ] Factory Droid").unique().expect();
  });
  await step("save is refused", () => terminal.write("\r"), async () => {
    await locate(terminal, "! 1 errors").unique().expect();
  });
  assert.deepEqual(fs.readFileSync(userConfig), configBefore, "refusing the last-target removal changed the user config");
  assert.equal(fs.existsSync(userLock), lockBefore !== null, "refusing the last-target removal changed lock presence");
  if (lockBefore) assert.deepEqual(fs.readFileSync(userLock), lockBefore, "refusing the last-target removal changed the user lock");

  await step("the diagnostic shows reader-upgrade guidance", () => terminal.write("e"), async () => {
    await locate(terminal, "Errors (1)").unique().expect();
    assert.match((await terminal.text()).replaceAll(/\s+/g, " "), /upgrade every reader of this file, rename the key to targets:/);
  });
  await step("close the errors screen", () => terminal.write("e"), async () => {
    await locate(terminal, /e errors/).unique().expect();
  });
  await quitTui(terminal);
};

// spec: config-mutation/target-list-edits-keep-the-authored-spelling
// spec: config-mutation/editing-an-inherited-user-list-copies-the-effective-list
const snapshot = defineFixture("targets-alias", ["tui"], 80, 24, { organization: "missing-lock" });
before(async () => {
  await snapshot.setup();
  configureGlobalFixture(snapshot.current(), "apiVersion: agent-marketplace/v4\nsources: {}\n");
});
after(snapshot.cleanup);

snapshot.test("the inherited target list is copied when edited", async (terminal) => {
  const fixture = snapshot.current();
  const userConfig = path.join(fixture.home, ".agents", "config.yaml");

  await step("settled fixture baseline", () => {}, async () => {
    await locate(terminal, /Pending sync\s+\(1\)/).unique().expect({ timeout: 30_000 });
  });
  await step("open the global harness editor", () => terminal.write("H"), async () => {
    await locate(terminal, /Editing:\s+global/).unique().expect();
    await locate(terminal, "> [•] Factory Droid").unique().expect();
    await locate(terminal, "[•] Claude Code").unique().expect();
  });
  await step("move to Claude Code", () => terminal.write("\x1b[B"), async () => {
    await locate(terminal, "> [•] Claude Code").unique().expect();
  });
  await step("move to Cursor", () => terminal.write("\x1b[B"), async () => {
    await locate(terminal, "> [ ] Cursor").unique().expect();
  });
  await step("add Cursor", () => terminal.write(" "), async () => {
    await locate(terminal, "> [•] Cursor").unique().expect();
  });
  await step("save the inherited selection", () => terminal.write("\r"), async () => {
    await locate(terminal, "Saved global harnesses").unique().expect();
  });
  const snapped = fs.readFileSync(userConfig, "utf8");
  assert.match(snapped, /^harnesses:\n/m, `the inherited list was not written as harnesses:\n${snapped}`);
  assert.match(snapped, /  - factory\n  - claude\n  - cursor\n/, `the effective inherited list was not copied:\n${snapped}`);
  assert.doesNotMatch(snapped, /^targets:/m);

  await quitTui(terminal);
});

// spec: config-mutation/removing-the-last-target-from-a-targets-list-saves-an-empty-list
const emptyTargets = defineFixture("targets-alias-empty", ["tui"], 80, 24, { organization: "missing-lock" });
before(async () => {
  await emptyTargets.setup();
  configureGlobalFixture(emptyTargets.current(), "apiVersion: agent-marketplace/v4\ntargets: [factory]\nsources: {}\n", "factory");
});
after(emptyTargets.cleanup);

emptyTargets.test("an authored targets list can be emptied and reloads with no machine targets", async (terminal) => {
  const fixture = emptyTargets.current();
  const userConfig = path.join(fixture.home, ".agents", "config.yaml");

  await step("settled fixture baseline", () => {}, async () => {
    await locate(terminal, /Pending sync\s+\(1\)/).unique().expect({ timeout: 30_000 });
  });
  await step("open the global harness editor", () => terminal.write("H"), async () => {
    await locate(terminal, /Editing:\s+global/).unique().expect();
    await locate(terminal, "> [•] Factory Droid").unique().expect();
  });
  await step("remove Factory", () => terminal.write(" "), async () => {
    await locate(terminal, "> [ ] Factory Droid").unique().expect();
  });
  await step("save targets empty", () => terminal.write("\r"), async () => {
    await locate(terminal, "Saved global harnesses").unique().expect();
  });
  const saved = fs.readFileSync(userConfig, "utf8");
  assert.match(saved, /^targets:\s*\[\s*\]\s*$/m, `the authored target list was not emptied:\n${saved}`);
  assert.doesNotMatch(saved, /^harnesses:/m);

  await step("reopen after the saved configuration reloads", () => terminal.write("H"), async () => {
    await locate(terminal, /Global:\s+none/).unique().expect();
    await locate(terminal, /Effective:\s+none/).unique().expect();
  });
  await step("close the harness editor", () => terminal.write("\x1b"), async () => {
    await locate(terminal, /e errors/).unique().expect();
  });
  await quitTui(terminal);
});

// spec: config-mutation/removing-the-last-target-without-targets-spelling-is-refused
const refusal = defineFixture("targets-alias-refusal", ["tui"], 80, 24, { organization: "missing-lock" });
before(async () => {
  await refusal.setup();
  configureGlobalFixture(refusal.current(), "apiVersion: agent-marketplace/v4\nharnesses: [factory]\nsources: {}\n", "factory");
});
after(refusal.cleanup);

refusal.test("removing the last target from a harnesses list is refused without writes", async (terminal) => {
  await refuseEmptyGlobalSelection(terminal, refusal.current());
});

refusal.test("removing the last inherited target is refused without writes", async (terminal) => {
  configureGlobalFixture(refusal.current(), "apiVersion: agent-marketplace/v4\nsources: {}\n", "factory");
  await refuseEmptyGlobalSelection(terminal, refusal.current());
});

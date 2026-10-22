import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { after, before } from "node:test";

import { defineFixture, locate, step, waitForExit } from "./helpers.js";

// spec: tui-interactions/previewing-a-source-writes-nothing
// spec: tui-interactions/picked-resources-install-at-the-chosen-scope
// spec: tui-interactions/blocked-source-is-refused-before-network-access
// spec: tui-interactions/moved-ref-refuses-the-stale-install
// spec: tui-interactions/cancelling-at-any-step-preserves-bytes
// spec: tui-interactions/malformed-and-empty-sources-are-not-installable
const journey = defineFixture("install-from-source", ["tui"], 80, 24, { adHocSource: true });
before(journey.setup);
after(journey.cleanup);

journey.test("previews, picks, rejects a moved ref, then installs the selected resources", async (terminal) => {
  const fixture = journey.current();
  const repoConfig = path.join(fixture.repo, ".agents", "config.yaml");
  const repoLock = path.join(fixture.repo, ".agents", "lock.yaml");
  const existingHarness = path.join(fixture.repo, ".factory", "skills", "beta-skill", "SKILL.md");
  const originalConfig = fs.readFileSync(repoConfig);
  const originalLock = fs.readFileSync(repoLock);
  const originalHarness = fs.readFileSync(existingHarness);
  const adHocSourceURL = fixture.adHocSourceURL;
  assert.ok(adHocSourceURL, "adHocSource fixture omitted its source URL");
  const assertUnchanged = () => {
    assert.deepEqual(fs.readFileSync(repoConfig), originalConfig);
    assert.deepEqual(fs.readFileSync(repoLock), originalLock);
    assert.deepEqual(fs.readFileSync(existingHarness), originalHarness);
  };

  await step("settled source list", () => {}, async () => {
    await locate(terminal, "~ in sync").unique().expect();
  });
  await step("open source modal", () => terminal.write("i"), async () => {
    await locate(terminal, "Install from source").unique().expect();
    await locate(terminal, "Source").unique().expect();
  });
  await step("enter blocked source", () => terminal.write("https://blocked.invalid/quick.git"), async () => {
    await locate(terminal, "blocked.invalid").expect();
  });
  await step("refuse blocked source before fetching", () => terminal.write("\r"), async () => {
    await locate(terminal, "blocked by allowed_registries").unique().expect();
    await locate(terminal, "Source").unique().expect();
  });
  assertUnchanged();
  await step("cancel blocked source", () => terminal.write("\x1b"), async () => {
    await locate(terminal, "~ in sync").unique().expect();
    await locate(terminal, "Install from source").expect({ not: true });
  });
  assertUnchanged();

  await step("reopen source modal", () => terminal.write("i"), async () => {
    await locate(terminal, "Install from source").unique().expect();
  });
  await step("enter source to cancel from Pick", () => terminal.write(adHocSourceURL), async () => {
    await locate(terminal, "source.git").unique().expect();
  });
  await step("preview before Pick cancellation", () => terminal.write("\r"), async () => {
    await locate(terminal, /3 resources at [0-9a-f]{7,40}/).unique().expect({ timeout: 30_000 });
  });
  await step("cancel from Pick", () => terminal.write("\x1b"), async () => {
    await locate(terminal, "~ in sync").unique().expect();
    await locate(terminal, "Install from source").expect({ not: true });
  });
  assertUnchanged();

  await step("reopen source modal for Review cancellation", () => terminal.write("i"), async () => {
    await locate(terminal, "Install from source").unique().expect();
  });
  await step("enter source for Review cancellation", () => terminal.write(adHocSourceURL), async () => {
    await locate(terminal, "source.git").unique().expect();
  });
  await step("preview before Review cancellation", () => terminal.write("\r"), async () => {
    await locate(terminal, "ad-hoc-alpha").expect();
  });
  await step("select a resource before Review cancellation", () => terminal.write(" "), async () => {
    await locate(terminal, "1 selected").unique().expect();
  });
  await step("enter Review before cancellation", () => terminal.write("\r"), async () => {
    await locate(terminal, "Review").unique().expect();
  });
  await step("cancel from Review", () => terminal.write("\x1b"), async () => {
    await locate(terminal, "~ in sync").unique().expect();
    await locate(terminal, "Install from source").expect({ not: true });
  });
  assertUnchanged();

  await step("open source modal for install", () => terminal.write("i"), async () => {
    await locate(terminal, "Install from source").unique().expect();
  });
  await step("enter unconfigured Git source", () => terminal.write(adHocSourceURL), async () => {
    await locate(terminal, "source.git").unique().expect();
  });
  await step("preview without writes", () => terminal.write("\r"), async () => {
    await locate(terminal, /3 resources at [0-9a-f]{7,40}/).unique().expect({ timeout: 30_000 });
    await locate(terminal, "ad-hoc-alpha").expect();
    await locate(terminal, "ad-hoc-command").expect();
    await locate(terminal, "ad-hoc-broken").expect();
    await locate(terminal, /\[!\] skill\s+ad-hoc-broken/).unique().expect();
    await locate(terminal, "malformed").expect();
  });
  assertUnchanged();

  await step("pick skill and command", () => terminal.write(" "), async () => {
    await locate(terminal, "1 selected").unique().expect();
  });
  await step("move to command", () => terminal.write("jjj"), async () => {
    await locate(terminal, /› \[ \] command\s+ad-hoc-command/).unique().expect();
  });
  await step("select command", () => terminal.write(" "), async () => {
    await locate(terminal, "2 selected").unique().expect();
  });
  await step("review project install", () => terminal.write("\r"), async () => {
    await locate(terminal, "Review").unique().expect();
    await locate(terminal, "ad-hoc-alpha").expect();
    await locate(terminal, "ad-hoc-command").expect();
    await locate(terminal, ".agents/config.yaml").expect();
  });
  await step("choose user scope", () => terminal.write("g"), async () => {
    await locate(terminal, /› \[g\] User/).unique().expect();
  });
  await step("choose project scope", () => terminal.write("p"), async () => {
    await locate(terminal, /› \[p\] Project/).unique().expect();
  });

  fixture.pushRegistryChange("commands/ad-hoc-command.md", "---\nname: ad-hoc-command\ndescription: Updated command\n---\n\nUpdated.\n");
  await step("refuse moved source", () => terminal.write("\r"), async () => {
    await locate(terminal, "moved from").unique().expect({ timeout: 30_000 });
    await locate(terminal, "preview again").unique().expect();
    await locate(terminal, "Pick").unique().expect();
    await locate(terminal, /3 resources at [0-9a-f]{7,40}/).unique().expect({ timeout: 30_000 });
  });
  assertUnchanged();

  await step("review refreshed source", () => terminal.write("\r"), async () => {
    await locate(terminal, "Selected resources:").unique().expect();
    await locate(terminal, "Writes:").unique().expect();
    await locate(terminal, "ad-hoc-alpha").expect();
    await locate(terminal, "ad-hoc-command").expect();
  });
  await step("confirm refreshed install", () => terminal.write("\r"), async () => {
    await locate(terminal, /Installed 2 from/).unique().expect({ timeout: 30_000 });
    await locate(terminal, "Install from source").expect({ not: true });
    await locate(terminal, "ad-hoc-alpha").expect();
    await locate(terminal, "ad-hoc-command").expect();
    await locate(terminal, /~ (?:in sync|2 out of sync)/).unique().expect({ timeout: 30_000 });
  });
  const configBytes = fs.readFileSync(repoConfig, "utf8");
  assert.match(configBytes, /ad-hoc-alpha/);
  assert.match(configBytes, /ad-hoc-command/);
  assert.match(fs.readFileSync(repoLock, "utf8"), /commit:/);

  await step("sync materializes selected resources", () => terminal.write("s"), async () => {
    await locate(terminal, "already in sync").unique().expect();
    await locate(terminal, "~ in sync").unique().expect({ timeout: 30_000 });
  });
  assert.equal(fs.existsSync(path.join(fixture.repo, ".factory", "skills", "ad-hoc-alpha", "SKILL.md")), true);
  assert.equal(fs.existsSync(path.join(fixture.repo, ".factory", "commands", "ad-hoc-command.md")), true);

  await step("quit after install", () => terminal.write("q"), async () => {
    assert.equal((await waitForExit(terminal)).exitCode, 0);
  });
});

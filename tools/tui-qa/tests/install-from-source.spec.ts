import { execFileSync } from "node:child_process";
import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { after, before } from "node:test";

import type { Fixture } from "./fixture.js";
import { defineFixture, locate, step, waitForExit, type TestTerminal } from "./helpers.js";
import { runAM } from "./process-helpers.js";

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

const RIGHT = "\x1b[C";
const LEFT = "\x1b[D";
const UP = "\x1b[A";
const DOWN = "\x1b[B";

const adHocURL = (fixture: Fixture): string => {
  assert.ok(fixture.adHocSourceURL, "adHocSource fixture omitted its source URL");
  return fixture.adHocSourceURL;
};

const snapshotRepo = (fixture: Fixture): (() => void) => {
  const files = [
    path.join(fixture.repo, ".agents", "config.yaml"),
    path.join(fixture.repo, ".agents", "lock.yaml"),
    path.join(fixture.repo, ".factory", "skills", "beta-skill", "SKILL.md"),
  ];
  const contents = files.map((file) => fs.readFileSync(file));
  return () => files.forEach((file, index) => assert.deepEqual(fs.readFileSync(file), contents[index]));
};

const openPreview = async (terminal: TestTerminal, source: string): Promise<void> => {
  await step("open source modal", () => terminal.write("i"), async () => {
    await locate(terminal, "Install from source").unique().expect();
  });
  await step("enter source", () => terminal.write(source), async () => {
    await locate(terminal, "Source").expect();
  });
  await step("preview source", () => terminal.write("\r"), async () => {
    await locate(terminal, /3 resources at/).unique().expect({ timeout: 30_000 });
    await locate(terminal, "ad-hoc-alpha").expect();
  });
};

const cancelAndQuit = async (terminal: TestTerminal): Promise<void> => {
  await step("cancel the modal", () => terminal.write("\x1b"), async () => {
    await locate(terminal, "~ in sync").unique().expect();
    await locate(terminal, "Install from source").expect({ not: true });
  });
  await step("quit", () => terminal.write("q"), async () => {
    assert.equal((await waitForExit(terminal)).exitCode, 0);
  });
};

// spec: tui-interactions/arrow-keys-walk-the-steps-without-installing
const walk = defineFixture("install-from-source-walk", ["tui"], 80, 24, { adHocSource: true });
before(walk.setup);
after(walk.cleanup);

walk.test("arrow keys walk the install steps without installing", async (terminal) => {
  const fixture = walk.current();
  const assertUnchanged = snapshotRepo(fixture);
  await step("settled source list", () => {}, async () => {
    await locate(terminal, "~ in sync").unique().expect();
  });
  await openPreview(terminal, adHocURL(fixture));
  await step("pick a skill", () => terminal.write(" "), async () => {
    await locate(terminal, "1 selected").unique().expect();
  });
  await step("right moves to Review", () => terminal.write(RIGHT), async () => {
    await locate(terminal, "Selected resources:").unique().expect();
  });
  await step("right on Review does not install", () => terminal.write(RIGHT), async () => {
    await locate(terminal, "Selected resources:").unique().expect();
    await locate(terminal, "Install from source").unique().expect();
  });
  assertUnchanged();
  await step("left returns to Pick", () => terminal.write(LEFT), async () => {
    await locate(terminal, "1 selected").unique().expect();
  });
  await step("left returns to Source", () => terminal.write(LEFT), async () => {
    await locate(terminal, "Web URLs fill ref and path automatically.").unique().expect();
  });
  assertUnchanged();
  await cancelAndQuit(terminal);
  assertUnchanged();
});

// spec: tui-interactions/unchanged-source-reuses-the-preview
const reuse = defineFixture("install-from-source-reuse", ["tui"], 80, 24, { adHocSource: true });
before(reuse.setup);
after(reuse.cleanup);

reuse.test("an unchanged source keeps its preview after the source gains a resource", async (terminal) => {
  const fixture = reuse.current();
  const assertUnchanged = snapshotRepo(fixture);
  await step("settled source list", () => {}, async () => {
    await locate(terminal, "~ in sync").unique().expect();
  });
  await openPreview(terminal, adHocURL(fixture));
  await step("pick a skill", () => terminal.write(" "), async () => {
    await locate(terminal, "1 selected").unique().expect();
  });
  fixture.pushRegistryChange("skills/ad-hoc-new/SKILL.md", "---\nname: ad-hoc-new\ndescription: Gained after preview\n---\n\n# Ad Hoc New\n");
  await step("return to Source", () => terminal.write(LEFT), async () => {
    await locate(terminal, "Web URLs fill ref and path automatically.").unique().expect();
  });
  await step("confirm the unchanged source", () => terminal.write("\r"), async () => {
    await locate(terminal, "1 selected").unique().expect();
    await locate(terminal, "ad-hoc-alpha").expect();
    await locate(terminal, "ad-hoc-new").expect({ not: true });
  });
  assertUnchanged();
  await cancelAndQuit(terminal);
  assertUnchanged();
});

// spec: tui-interactions/changed-source-previews-again
const changed = defineFixture("install-from-source-path", ["tui"], 80, 24, { adHocSource: true });
before(changed.setup);
after(changed.cleanup);

changed.test("a changed source previews again", async (terminal) => {
  const fixture = changed.current();
  const url = adHocURL(fixture);
  const assertUnchanged = snapshotRepo(fixture);
  await step("settled source list", () => {}, async () => {
    await locate(terminal, "~ in sync").unique().expect();
  });
  await openPreview(terminal, url);
  await step("return to Source", () => terminal.write(LEFT), async () => {
    await locate(terminal, "Web URLs fill ref and path automatically.").unique().expect();
  });
  await step("tab moves typing into Ref", async () => {
    await terminal.write("\t");
    await terminal.write("x");
  }, async () => {
    await locate(terminal, "> mainx").unique().expect();
  });
  await step("restore Ref", () => terminal.write("\x7f"), async () => {
    await locate(terminal, "> main").unique().expect();
  });
  await step("tab moves typing into Path", async () => {
    await terminal.write("\t");
    await terminal.write("skills");
  }, async () => {
    await locate(terminal, "> skills").unique().expect();
  });
  await step("preview the skills path", () => terminal.write("\r"), async () => {
    await locate(terminal, "ad-hoc-second").unique().expect({ timeout: 30_000 });
    await locate(terminal, "ad-hoc-command").expect({ not: true });
  });
  assertUnchanged();
  await cancelAndQuit(terminal);
  assertUnchanged();
});

// spec: tui-interactions/arrow-keys-move-between-source-fields
const fields = defineFixture("install-from-source-fields", ["tui"], 80, 24, { adHocSource: true });
before(fields.setup);
after(fields.cleanup);

fields.test("arrow keys move typing between Source fields without leaving the step", async (terminal) => {
  const fixture = fields.current();
  const assertUnchanged = snapshotRepo(fixture);
  await step("settled source list", () => {}, async () => {
    await locate(terminal, "~ in sync").unique().expect();
  });
  await step("open source modal", () => terminal.write("i"), async () => {
    await locate(terminal, "Web URLs fill ref and path automatically.").unique().expect();
  });
  await step("up on URL keeps focus on URL", () => terminal.write(UP), async () => {
    await locate(terminal, "Web URLs fill ref and path automatically.").unique().expect();
  });
  await step("type into URL", () => terminal.write("u"), async () => {
    await locate(terminal, "> u").unique().expect();
  });
  await step("down moves to Ref", () => terminal.write(DOWN), async () => {
    await locate(terminal, "Web URLs fill ref and path automatically.").unique().expect();
  });
  await step("down moves to Path", () => terminal.write(DOWN), async () => {
    await locate(terminal, "Web URLs fill ref and path automatically.").unique().expect();
  });
  await step("type into Path", () => terminal.write("p"), async () => {
    await locate(terminal, "> p").unique().expect();
  });
  await step("down on Path keeps focus on Path", () => terminal.write(DOWN), async () => {
    await locate(terminal, "Web URLs fill ref and path automatically.").unique().expect();
  });
  await step("type again into Path", () => terminal.write("q"), async () => {
    await locate(terminal, "> pq").unique().expect();
  });
  await step("up moves back to Ref", () => terminal.write(UP), async () => {
    await locate(terminal, "Web URLs fill ref and path automatically.").unique().expect();
  });
  await step("type into Ref", () => terminal.write("r"), async () => {
    await locate(terminal, "> r").unique().expect();
    await locate(terminal, "> u").unique().expect();
    await locate(terminal, "Previewing source…").expect({ not: true });
  });
  assertUnchanged();
  await cancelAndQuit(terminal);
  assertUnchanged();
});

// spec: tui-interactions/arrow-keys-choose-the-review-scope
const scope = defineFixture("install-from-source-scope", ["tui"], 80, 24, { adHocSource: true });
before(scope.setup);
after(scope.cleanup);

scope.test("arrow keys choose the Local review scope and write the local configuration", async (terminal) => {
  const fixture = scope.current();
  await step("settled source list", () => {}, async () => {
    await locate(terminal, "~ in sync").unique().expect();
  });
  await openPreview(terminal, adHocURL(fixture));
  await step("pick a skill", () => terminal.write(" "), async () => {
    await locate(terminal, "1 selected").unique().expect();
  });
  await step("review the install", () => terminal.write("\r"), async () => {
    await locate(terminal, "Selected resources:").unique().expect();
    await locate(terminal, /› \[p\] Project/).unique().expect();
  });
  await step("move the scope down to Local", () => terminal.write(DOWN), async () => {
    await locate(terminal, /› \[l\] Local/).unique().expect();
    await locate(terminal, /› \[p\] Project/).expect({ not: true });
  });
  await step("confirm the Local install", () => terminal.write("\r"), async () => {
    await locate(terminal, /Installed 1 from/).unique().expect({ timeout: 30_000 });
    await locate(terminal, "Install from source").expect({ not: true });
  });
  const localConfig = fs.readFileSync(path.join(fixture.repo, ".agents", "config.local.yaml"), "utf8");
  assert.match(localConfig, /ad-hoc-alpha/);
  assert.ok(localConfig.includes(adHocURL(fixture)), `config.local.yaml does not record the source:\n${localConfig}`);
  await step("sync the Local install", () => terminal.write("s"), async () => {
    await locate(terminal, "~ in sync").unique().expect({ timeout: 30_000 });
  });
  assert.equal(fs.existsSync(path.join(fixture.repo, ".factory", "skills", "ad-hoc-alpha", "SKILL.md")), true);
  await step("quit", () => terminal.write("q"), async () => {
    assert.equal((await waitForExit(terminal)).exitCode, 0);
  });
});

// spec: tui-interactions/arrow-keys-skip-an-unwritable-scope
const refusal = defineFixture("install-from-source-refusal", ["tui"], 80, 24, { adHocSource: true });
before(refusal.setup);
after(refusal.cleanup);

refusal.test("up from Project keeps Project when user configuration cannot take the source", async (terminal) => {
  const fixture = refusal.current();
  const assertUnchanged = snapshotRepo(fixture);
  fs.cpSync(path.join(fixture.root, "registries", "ad-hoc-source"), path.join(fixture.repo, "local-source"), {
    recursive: true,
    filter: (source) => path.basename(source) !== ".git",
  });
  await step("settled source list", () => {}, async () => {
    await locate(terminal, "~ in sync").unique().expect();
  });
  await step("open source modal", () => terminal.write("i"), async () => {
    await locate(terminal, "Install from source").unique().expect();
  });
  await step("enter repo-relative directory", () => terminal.write("./local-source"), async () => {
    await locate(terminal, "local-source").expect();
  });
  await step("preview the directory", () => terminal.write("\r"), async () => {
    await locate(terminal, /3 resources at/).unique().expect({ timeout: 30_000 });
    await locate(terminal, "ad-hoc-alpha").expect();
  });
  await step("pick a skill", () => terminal.write(" "), async () => {
    await locate(terminal, "1 selected").unique().expect();
  });
  await step("review the install", () => terminal.write(RIGHT), async () => {
    await locate(terminal, "Selected resources:").unique().expect();
    await locate(terminal, /› \[p\] Project/).unique().expect();
  });
  await step("move the scope up from Project", () => terminal.write(UP), async () => {
    await locate(terminal, /› \[p\] Project/).unique().expect();
    await locate(terminal, "repo-relative").expect();
  });
  assertUnchanged();
  await cancelAndQuit(terminal);
  assertUnchanged();
});

const headOf = (repo: string): string => execFileSync("git", ["rev-parse", "HEAD"], { cwd: repo, encoding: "utf8" }).trim();

const lockPinned = defineFixture("install-from-source-lock-pinned", ["tui"], 80, 24, { adHocSource: true });
let lockPinnedCommits: { locked: string; advanced: string } | undefined;
before(async () => {
  await lockPinned.setup();
  const fixture = lockPinned.current();
  const seed = await runAM(fixture, ["install", "--from", adHocURL(fixture), "ad-hoc-alpha", "--scope", "repo"]);
  if (seed.status !== 0) throw new Error(`seed install failed (exit ${seed.status}):\n${seed.stdout}\n${seed.stderr}`);
  const locked = headOf(path.join(fixture.root, "registries", "ad-hoc-source"));
  fixture.pushRegistryChange("README.md", "Advanced past the locked commit.\n");
  lockPinnedCommits = { locked, advanced: headOf(path.join(fixture.root, "registries", "ad-hoc-source")) };
});
after(lockPinned.cleanup);

// spec: tui-interactions/lock-pinned-source-returns-to-review-with-update-remediation
// spec: tui-interactions/lock-pinned-source-installs-into-another-scope
lockPinned.test("a lock-pinned source returns to Review with update remediation", async (terminal) => {
  const fixture = lockPinned.current();
  assert.ok(lockPinnedCommits, "lock-pinned fixture did not record its commits");
  const { locked, advanced } = lockPinnedCommits;
  const repoFiles = [path.join(fixture.repo, ".agents", "config.yaml"), path.join(fixture.repo, ".agents", "lock.yaml")];
  const contents = repoFiles.map((file) => fs.readFileSync(file));
  const assertRepoUnchanged = () => repoFiles.forEach((file, index) => assert.deepEqual(fs.readFileSync(file), contents[index]));
  await step("settled source list", () => {}, async () => {
    await locate(terminal, "~ in sync").unique().expect();
  });
  await step("open source modal", () => terminal.write("i"), async () => {
    await locate(terminal, "Install from source").unique().expect();
  });
  await step("enter the configured source", () => terminal.write(adHocURL(fixture)), async () => {
    await locate(terminal, "source.git").unique().expect();
  });
  await step("preview shows the advanced commit", () => terminal.write("\r"), async () => {
    await locate(terminal, `resources at ${advanced.slice(0, 8)}`).unique().expect({ timeout: 30_000 });
  });
  await step("move to ad-hoc-second", () => terminal.write("j"), async () => {
    await locate(terminal, /› \[ \] skill\s+ad-hoc-second/).unique().expect();
  });
  await step("pick ad-hoc-second", () => terminal.write(" "), async () => {
    await locate(terminal, "1 selected").unique().expect();
  });
  await step("review the project install", () => terminal.write("\r"), async () => {
    await locate(terminal, "Selected resources:").unique().expect();
    await locate(terminal, /› \[p\] Project/).unique().expect();
  });
  assertRepoUnchanged();
  await step("confirm the project install", () => terminal.write("\r"), async () => {
    await locate(terminal, "lock pins").unique().expect({ timeout: 30_000 });
    await locate(terminal, `${locked.slice(0, 8)}`).expect();
    await locate(terminal, `${advanced.slice(0, 8)}`).expect();
    await locate(terminal, "update --registry").expect();
    await locate(terminal, "--scope repo").expect();
    await locate(terminal, "Selected resources:").unique().expect();
    await locate(terminal, "preview again").expect({ not: true });
    await locate(terminal, "another scope").expect();
  });
  assertRepoUnchanged();
  await step("choose another scope", () => terminal.write("l"), async () => {
    await locate(terminal, /› \[l\] Local/).unique().expect();
  });
  await step("confirm the Local install", () => terminal.write("\r"), async () => {
    await locate(terminal, /Installed 1 from/).unique().expect({ timeout: 30_000 });
    await locate(terminal, "Install from source").expect({ not: true });
  });
  assert.match(fs.readFileSync(path.join(fixture.repo, ".agents", "config.local.yaml"), "utf8"), /ad-hoc-second/);
  assertRepoUnchanged();
  await step("sync the installs", () => terminal.write("s"), async () => {
    await locate(terminal, "~ in sync").unique().expect({ timeout: 30_000 });
  });
  await step("quit", () => terminal.write("q"), async () => {
    assert.equal((await waitForExit(terminal)).exitCode, 0);
  });
});

import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import fs from "node:fs";
import path from "node:path";
import { after, before } from "node:test";

import { defineFixture, locate, step, waitForExit } from "./helpers.js";
import { readRegularFile, runAM, write } from "./process-helpers.js";

const assertContent = (file: string, expected: string): void => {
  let actual: Buffer | undefined;
  assert.doesNotThrow(() => { actual = readRegularFile(file); }, `expected readable fixture file: ${file}`);
  assert.deepEqual(actual, Buffer.from(expected), file);
};

const snapshot = (roots: string[]): Map<string, string> => {
  const files = new Map<string, string>();
  const visit = (file: string): void => {
    const stat = fs.lstatSync(file, { bigint: true, throwIfNoEntry: false });
    if (!stat) {
      files.set(file, "absent");
      return;
    }
    // Legitimate reads may update atime, but not ownership or mutation metadata.
    const metadata = `${stat.mode}:${stat.uid}:${stat.gid}:${stat.ino}:${stat.nlink}:${stat.mtimeNs}:${stat.ctimeNs}`;
    if (stat.isSymbolicLink()) files.set(file, `link:${metadata}:${fs.readlinkSync(file)}`);
    else if (stat.isDirectory()) {
      files.set(file, `directory:${metadata}`);
      for (const name of fs.readdirSync(file)) visit(path.join(file, name));
    } else {
      assert.ok(stat.isFile(), `unexpected special file ${file}`);
      files.set(file, `file:${metadata}:${createHash("sha256").update(readRegularFile(file)).digest("hex")}`);
    }
  };
  for (const root of roots) visit(root);
  return files;
};

for (const relative of [false, true]) {
  const label = relative ? "relative" : "absolute";
  const journey = defineFixture(`local-source-browsing-${label}`, ["tui"], 80, 24, { directorySource: true });
  let source: string;
  before(async () => {
    await journey.setup();
    const fixture = journey.current();
    source = relative ? path.join(fixture.repo, "registry") : fixture.registryURL;
    if (relative) {
      fs.cpSync(fixture.registryURL, source, { recursive: true });
      write(path.join(fixture.home, ".agents", "config.yaml"), "apiVersion: agent-marketplace/v4\nharnesses: [factory, gemini, devin]\nsources: {}\n");
      write(path.join(fixture.repo, ".agents", "config.yaml"), "apiVersion: agent-marketplace/v4\nsources:\n  local:\n    directory: ./registry\nresources:\n  local:\n    include: [skill/beta-skill]\n");
    } else {
      fs.appendFileSync(path.join(fixture.home, ".agents", "config.yaml"), "resources:\n  local:\n    include: [skill/beta-skill]\n");
    }
    write(path.join(source, "skills", "alpha-skill", "notes.md"), "# Notes\n\nRegistry supporting instructions.\n");
    write(path.join(source, "commands", "verify.md"), "---\nname: verify\ndescription: Local verification\n---\n\nRegistry command instructions.\n");
    for (const file of ["skills/alpha-skill/SKILL.md", "skills/alpha-skill/notes.md", "commands/verify.md"]) {
      write(path.join(fixture.repo, file), "CONSUMER DECOY MUST NOT BE PREVIEWED\n");
    }
    const synced = await runAM(fixture, ["sync"]);
    assert.equal(synced.status, 0, `${synced.stdout}\n${synced.stderr}`);
  });
  after(journey.cleanup);

  // spec: tui-navigation/local-disk-resources-remain-browsable-outside-their-registry
  // spec: tui-navigation/consumer-files-cannot-substitute-local-source-content
  // spec: tui-navigation/missing-or-unsafe-local-content-fails-read-only
  journey.test(`${label} local sources browse their own skill and command, and refuse missing or unsafe payloads`, async (terminal) => {
    const fixture = journey.current();
    await step("settled local source and real materialized control", () => {}, async () => {
      await locate(terminal, "~ in sync").unique().expect({ timeout: 30_000 });
      await locate(terminal, /Enabled\s+\(1\)/).unique().expect();
      await locate(terminal, /Available\s+\(2\)/).unique().expect();
    });
    const nativeRoot = relative ? fixture.repo : fixture.home;
    const generated = [".factory", ".gemini", relative ? ".devin" : ".config/devin"].map((harness) => {
      const installed = path.join(nativeRoot, harness, "skills", "beta-skill");
      assert.equal(fs.lstatSync(installed).isSymbolicLink(), true, installed);
      const target = fs.realpathSync(installed);
      assert.ok(target.startsWith(`${fs.realpathSync(fixture.root)}${path.sep}`), "native control escapes the task-owned fixture");
      assertContent(path.join(target, "SKILL.md"), fixture.expectedBetaSkillContent);
      return target;
    });
    const primary = path.join(source, "skills", "alpha-skill", "SKILL.md");
    const command = path.join(source, "commands", "verify.md");
    const external = path.join(fixture.root, "unsafe-payload.md");
    write(external, "UNSAFE LOCAL PAYLOAD MUST NOT BE PREVIEWED\n", 0o640);
    const controlled = [
      source, fixture.repo, external, ...generated,
      path.join(fixture.home, ".agents"),
      path.join(fixture.home, ".factory"),
      path.join(fixture.home, ".gemini"),
      path.join(fixture.home, ".config", "devin")
    ];
    let baseline = snapshot(controlled);
    const unchanged = (): void => assert.deepEqual(snapshot(controlled), baseline, "browsing changed controlled files or metadata");
    const noDecoy = async (): Promise<void> => {
      await locate(terminal, "CONSUMER DECOY").expect({ not: true });
      await locate(terminal, "UNSAFE LOCAL PAYLOAD").expect({ not: true });
    };
    const select = async (query: string): Promise<void> => {
      await step(`search ${query}`, () => terminal.write(`/${query}`), async () => {
        await locate(terminal, `/ ${query}`).unique().expect();
        await locate(terminal, /enter apply.*esc cancel/).unique().expect();
      });
      await step(`select ${query}`, () => terminal.write("\r"), async () => {
        await locate(terminal, `/ ${query}  1 match`).unique().expect();
        await locate(terminal, new RegExp(`Name:\\s+${query}`)).unique().expect();
        await locate(terminal, /enter apply.*esc cancel/).expect({ not: true });
      });
    };
    const back = async (name: string): Promise<void> => {
      await step("return to the selected resource", () => terminal.write("\x1b"), async () => {
        await locate(terminal, new RegExp(`Name:\\s+${name}`)).unique().expect();
      });
      unchanged();
    };
    const clear = async (): Promise<void> => {
      await step("clear the applied filter", () => terminal.write("\x1b"), async () => {
        await locate(terminal, "esc clear").expect({ not: true });
        await locate(terminal, /Available\s+\(2\)/).unique().expect();
      });
    };

    await select("alpha-skill");
    await step("open the source primary, not the consumer copy", () => terminal.write("\r"), async () => {
      await locate(terminal, "SKILL.md").unique().expect();
      await locate(terminal, "notes.md").unique().expect();
      await locate(terminal, "Alpha fixture body.").unique().expect();
      await noDecoy();
    });
    await step("browse the source supporting file", () => terminal.write("j"), async () => {
      await locate(terminal, "Registry supporting instructions.").unique().expect();
      await locate(terminal, "Alpha fixture body.").expect({ not: true });
      await noDecoy();
    });
    await back("alpha-skill");
    await clear();
    await select("verify");
    await step("open the file-backed source", () => terminal.write("\r"), async () => {
      await locate(terminal, "verify.md").unique().expect();
      await locate(terminal, "Registry command instructions.").unique().expect();
      await locate(terminal, "SKILL.md").expect({ not: true });
      await noDecoy();
    });
    await back("verify");

    // Change only task-owned fixture files after discovery, without a reload.
    fs.unlinkSync(command);
    baseline = snapshot(controlled);
    await step("a disappeared primary fails instead of reading the consumer decoy", () => terminal.write("\r"), async () => {
      await locate(terminal, /Error reading resource:/).unique().expect();
      await locate(terminal, "Registry command instructions.").expect({ not: true });
      await noDecoy();
    });
    await back("verify");
    await clear();
    await select("alpha-skill");
    unchanged();
    fs.unlinkSync(primary);
    fs.linkSync(external, primary);
    assert.equal(fs.statSync(primary).nlink, 2, "unsafe-payload control is not multiply linked");
    baseline = snapshot(controlled);
    await step("a multiply linked primary fails without displaying unsafe bytes", () => terminal.write("\r"), async () => {
      await locate(terminal, /Error reading file:/).unique().expect();
      await locate(terminal, "Alpha fixture body.").expect({ not: true });
      await noDecoy();
    });
    await back("alpha-skill");
    await step("quit", () => terminal.write("q"), async () => {
      assert.equal((await waitForExit(terminal)).exitCode, 0);
    });
    unchanged();
  });
}

const marketplace = defineFixture("installed-marketplace-browsing", ["tui"], 80, 24, { organization: "published-lock" });
before(marketplace.setup);
after(marketplace.cleanup);

// spec: session-navigation/installed-skill-definition-and-supporting-files-remain-browsable
marketplace.test("installed Marketplace content previews its definition and staged support, and fails read-only when unavailable", async (terminal) => {
  const fixture = marketplace.current();
  assert.ok(fixture.marketplaceArchiveDigest);
  assert.ok(fixture.orgConfigPath);
  assert.ok(fixture.expectedOrgSkillContent);
  // Startup may restage cached support; check its files only after verification.
  await step("settled installed Marketplace fixture", () => {}, async () => {
    await locate(terminal, "~ in sync").unique().expect({ timeout: 30_000 });
    await locate(terminal, /Required\s+\(1\)/).unique().expect();
  });
  const digest = fixture.marketplaceArchiveDigest.replace(":", "-");
  const staged = path.join(fixture.cacheDir, "marketplace", "compiled", "marketplace-artifacts", digest);
  const notes = path.join(staged, "notes.md");
  const installed = path.join(fixture.home, ".factory", "skills", "org-review");
  assert.equal(fs.lstatSync(path.join(staged, "SKILL.md"), { throwIfNoEntry: false }), undefined, "the fixture must keep its canonical definition separate from staged support");
  assertContent(notes, "# Review Notes\n\nVerified supporting instructions.\n");
  assertContent(path.join(installed, "notes.md"), "# Review Notes\n\nVerified supporting instructions.\n");
  const generated = fs.realpathSync(installed);
  assert.ok(generated.startsWith(`${fs.realpathSync(fixture.root)}${path.sep}`), "installed Marketplace control escapes the task-owned fixture");
  assertContent(path.join(generated, "SKILL.md"), fixture.expectedOrgSkillContent);
  const controlled = [
    path.dirname(staged), generated, fixture.repo,
    ...["artifacts", "placeholder-artifacts"].map((form) => path.join(fixture.cacheDir, "marketplace", form, digest)),
    path.join(fixture.home, ".agents"),
    path.join(fixture.home, ".factory"),
    path.dirname(fixture.orgConfigPath)
  ];
  assert.ok(fixture.marketplaceAcquisitionDataRequestCount);
  const acquired = fixture.marketplaceAcquisitionDataRequestCount();
  assert.ok(acquired > 0, "the fixture must actually acquire verified Marketplace content");
  let baseline = snapshot(controlled);
  const unchanged = (): void => assert.deepEqual(snapshot(controlled), baseline, "Marketplace browsing changed controlled files or metadata");
  const noCacheFiles = async (): Promise<void> => {
    await locate(terminal, "artifact.tar.gz").expect({ not: true });
    await locate(terminal, "verified.marker").expect({ not: true });
    await locate(terminal, ".cache").expect({ not: true });
  };
  await step("search the installed Marketplace skill", () => terminal.write("/org-review"), async () => {
    await locate(terminal, "/ org-review").unique().expect();
    await locate(terminal, /enter apply.*esc cancel/).unique().expect();
  });
  await step("select the installed Marketplace skill", () => terminal.write("\r"), async () => {
    await locate(terminal, "/ org-review  1 match").unique().expect();
    await locate(terminal, /Name:\s+org-review/).unique().expect();
    await locate(terminal, /enter apply.*esc cancel/).expect({ not: true });
  });
  await step("open the canonical definition alongside staged support", () => terminal.write("\r"), async () => {
    await locate(terminal, "SKILL.md").unique().expect();
    await locate(terminal, "notes.md").unique().expect();
    await locate(terminal, "Review organization changes carefully.").unique().expect();
    await noCacheFiles();
  });
  await step("preview the installed supporting file", () => terminal.write("j"), async () => {
    await locate(terminal, "Verified supporting instructions.").unique().expect();
    await locate(terminal, "Review organization changes carefully.").expect({ not: true });
    await noCacheFiles();
  });
  await step("return to the canonical definition", () => terminal.write("k"), async () => {
    await locate(terminal, "Review organization changes carefully.").unique().expect();
  });
  await step("return to the installed row", () => terminal.write("\x1b"), async () => {
    await locate(terminal, /Name:\s+org-review/).unique().expect();
  });
  unchanged();

  const unavailable = `${staged}-unavailable`;
  fs.renameSync(staged, unavailable);
  baseline = snapshot(controlled);
  await step("missing staged content fails visibly without a cache tree", () => terminal.write("\r"), async () => {
    await locate(terminal, /Error reading resource:/).unique().expect();
    await locate(terminal, "Review organization changes carefully.").expect({ not: true });
    await locate(terminal, "Verified supporting instructions.").expect({ not: true });
    await locate(terminal, "SKILL.md").expect({ not: true });
    await locate(terminal, "notes.md").expect({ not: true });
    await noCacheFiles();
  });
  await step("leave the missing-content view", () => terminal.write("\x1b"), async () => {
    await locate(terminal, /Name:\s+org-review/).unique().expect();
  });
  unchanged();
  fs.symlinkSync(unavailable, staged, "dir");
  assert.equal(fs.lstatSync(staged).isSymbolicLink(), true);
  assertContent(path.join(unavailable, "notes.md"), "# Review Notes\n\nVerified supporting instructions.\n");
  baseline = snapshot(controlled);
  await step("an unsafe staged root fails instead of following its live target", () => terminal.write("\r"), async () => {
    await locate(terminal, /Error reading resource:/).unique().expect();
    await locate(terminal, "Review organization changes carefully.").expect({ not: true });
    await locate(terminal, "Verified supporting instructions.").expect({ not: true });
    await locate(terminal, "SKILL.md").expect({ not: true });
    await locate(terminal, "notes.md").expect({ not: true });
    await noCacheFiles();
  });
  await step("return to the installed resource", () => terminal.write("\x1b"), async () => {
    await locate(terminal, /Name:\s+org-review/).unique().expect();
  });
  await step("quit", () => terminal.write("q"), async () => {
    assert.equal((await waitForExit(terminal)).exitCode, 0);
  });
  unchanged();
  assert.equal(fixture.marketplaceAcquisitionDataRequestCount(), acquired, "browsing reacquired Marketplace content");
});

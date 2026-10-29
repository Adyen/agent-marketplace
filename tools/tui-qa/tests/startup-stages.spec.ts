import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { after, before } from "node:test";

import { defineFixture, locate, step, waitForExit } from "./helpers.js";
import { readRegularFile, write } from "./process-helpers.js";

const assertContent = (file: string, expected: string): void => {
  let actual: Buffer | undefined;
  assert.doesNotThrow(() => { actual = readRegularFile(file); }, `expected readable materialized skill: ${file}`);
  assert.deepEqual(actual, Buffer.from(expected), file);
};

let markBlocked: () => void;
const blocked = new Promise<void>((resolve) => { markBlocked = resolve; });
let releaseAcquisition: () => void;
const acquisition = new Promise<void>((resolve) => { releaseAcquisition = resolve; });
const snapshot = (files: string[]) => files.map((file) => {
  const stat = fs.lstatSync(file, { bigint: true, throwIfNoEntry: false });
  return [file, stat ? {
    bytes: readRegularFile(file), mode: stat.mode, uid: stat.uid, gid: stat.gid,
    ino: stat.ino, links: stat.nlink, mtime: stat.mtimeNs, ctime: stat.ctimeNs
  } : undefined];
});
let configuredFiles: string[];
let baseline: ReturnType<typeof snapshot>;
const journey = defineFixture("startup-stages", ["tui"], 80, 24, {
  organization: "missing-lock",
  directorySource: true,
  marketplaceAcquisitionGate: () => {
    markBlocked();
    return acquisition;
  }
});
before(async () => {
  await journey.setup();
  const fixture = journey.current();
  assert.ok(fixture.orgConfigPath);
  assert.ok(fixture.orgLockPath);
  assert.ok(fixture.marketplaceContentDigest);
  write(fixture.orgConfigPath,
    `apiVersion: agent-marketplace/v4\nsources:\n  org-market:\n    marketplace: ${fixture.marketplaceURL}\nresources:\n  org-market:\n    include:\n      - tools/skill/org-review:\n          revision: ${fixture.marketplaceContentDigest}\n    required: true\n`);
  fs.appendFileSync(path.join(fixture.home, ".agents", "config.yaml"), "resources:\n  local:\n    all: true\n");
  const projectSource = path.join(fixture.root, "project-source");
  for (const name of ["project-skill", "committed-hidden", "personal-hidden"]) {
    write(path.join(projectSource, "skills", name, "SKILL.md"), `---\nname: ${name}\ndescription: Project startup control\n---\n\nProject control.\n`);
  }
  for (const [file, name] of [["config.yaml", "committed-hidden"], ["config.local.yaml", "personal-hidden"]]) {
    write(path.join(fixture.repo, ".agents", file),
      `apiVersion: agent-marketplace/v4\nsources:\n  project:\n    directory: ${projectSource}\nresources:\n  project:\n${file === "config.yaml" ? "    all: true\n" : ""}    exclude: [skill/${name}]\n`);
  }
  configuredFiles = [
    path.join(fixture.home, ".agents", "config.yaml"),
    path.join(fixture.repo, ".agents", "config.yaml"),
    path.join(fixture.repo, ".agents", "config.local.yaml"),
    fixture.orgConfigPath, fixture.orgLockPath
  ];
  baseline = snapshot(configuredFiles);
});
after(() => {
  releaseAcquisition();
  journey.cleanup();
});

// spec: tui-startup/provisional-config-stage-publishes-verification-pending-rows
// spec: tui-startup/provisional-rows-render-while-transient-acquisition-is-blocked
// spec: tui-startup/local-all-true-selections-expand-into-pending-rows-on-the-normal-config-path
journey.test("startup shows guarded configured rows while acquisition is blocked, then verifies and syncs them", async (terminal) => {
  const fixture = journey.current();
  assert.ok(fixture.marketplaceAcquisitionDataRequestCount);
  const locks = ["lock.yaml", "lock.local.yaml"].flatMap((name) => [
    path.join(fixture.home, ".agents", name), path.join(fixture.repo, ".agents", name)
  ]);
  const coldLocks = snapshot(locks);
  const directories = [path.join(fixture.home, ".agents"), path.join(fixture.repo, ".agents")];
  const entries = directories.map((directory) => fs.readdirSync(directory).sort());
  const outputs = [
    ...[".factory", ".gemini", ".config/devin"].flatMap((harness) =>
      ["alpha-skill", "beta-skill", "project-skill", "committed-hidden", "personal-hidden"].map((name) =>
        path.join(fixture.home, harness, "skills", name))),
    path.join(fixture.home, ".factory", "skills", "org-review"),
    ...[".factory", ".gemini", ".devin"].flatMap((harness) =>
      ["project-skill", "committed-hidden", "personal-hidden"].map((name) => path.join(fixture.repo, harness, "skills", name)))
  ];
  const unchanged = (): void => {
    assert.deepEqual(snapshot(configuredFiles), baseline, "startup changed authored configuration or organization policy");
    assert.deepEqual(snapshot(locks), coldLocks, "a guarded action published a consumer lock");
    assert.deepEqual(directories.map((directory) => fs.readdirSync(directory).sort()), entries, "a guarded action left staging residue");
    for (const file of outputs) assert.equal(fs.lstatSync(file, { throwIfNoEntry: false }), undefined, `premature materialization: ${file}`);
  };
  let timer: ReturnType<typeof setTimeout> | undefined;
  try {
    await Promise.race([
      blocked,
      new Promise<never>((_, reject) => { timer = setTimeout(() => reject(new Error("startup did not reach the acquisition gate")), 5_000); })
    ]);
  } finally {
    clearTimeout(timer);
  }
  const checked = String.fromCodePoint(0x25cf);
  await step("configured rows render before acquisition can finish", () => {}, async () => {
    await locate(terminal, /Enabled\s+\(4\)/).unique().expect();
    for (const name of ["alpha-skill", "beta-skill", "org-review"]) {
      await locate(terminal, new RegExp(`${checked}\\s+G\\s+skill\\s+[^\\n]*/${name}`)).unique().expect();
    }
    await locate(terminal, new RegExp(`${checked}\\s+P\\s+skill\\s+project-source/project-skill`)).unique().expect();
    for (const name of ["committed-hidden", "personal-hidden"]) await locate(terminal, name).expect({ not: true });
    await locate(terminal, "verifying resources...").unique().expect();
    await locate(terminal, /~ (in sync|\d+ out of sync)/).expect({ not: true });
  });
  const pending = async (name: string): Promise<void> => {
    await locate(terminal, new RegExp(`Name:\\s+${name}`)).unique().expect();
    await locate(terminal, /Status:\s+enabled in config; verification\s+pending/).unique().expect();
    await locate(terminal, "verifying resources...").unique().expect();
    await locate(terminal, /toggle (global|project|local)/).expect({ not: true });
    await locate(terminal, /enter apply.*esc cancel/).expect({ not: true });
  };
  const select = async (name: string, selected: () => Promise<void> = () => pending(name)): Promise<void> => {
    await step(`search ${name}`, () => terminal.write(`/${name}`), async () => {
      await locate(terminal, `/ ${name}`).unique().expect();
      await locate(terminal, /enter apply.*esc cancel/).unique().expect();
    });
    await step(`apply ${name} filter`, () => terminal.write("\r"), async () => {
      await locate(terminal, `/ ${name}  1 match`).unique().expect();
      await locate(terminal, /enter apply.*esc cancel/).expect({ not: true });
      await selected();
    });
  };
  const guarded = async (key: string, name: string): Promise<void> => {
    await step(`refuse ${JSON.stringify(key)} before verification`, () => terminal.write(key), () => pending(name));
    // A help round-trip acknowledges input processing before the no-write check.
    await step("help remains reachable during verification", () => terminal.write("?"), async () => {
      await locate(terminal, "Keyboard Shortcuts").unique().expect();
      await locate(terminal, "fuzzy search").unique().expect();
    });
    await step("return from help to the pending selection", () => terminal.write("?"), async () => {
      await locate(terminal, "Keyboard Shortcuts").expect({ not: true });
      await pending(name);
    });
    unchanged();
    assert.equal(fixture.marketplaceAcquisitionDataRequestCount?.(), 1, "a guarded action started another acquisition");
  };
  unchanged();
  await select("org-review");
  await guarded(" ", "org-review");
  await step("clear the pin filter", () => terminal.write("\x1b"), async () => {
    await locate(terminal, "esc clear").expect({ not: true });
    await locate(terminal, /Enabled\s+\(4\)/).unique().expect();
  });
  await select("alpha-skill");
  for (const key of [" ", "s", "u"]) await guarded(key, "alpha-skill");
  await step("release acquisition and replace pending rows with verified inventory", releaseAcquisition, async () => {
    await locate(terminal, "~ 4 out of sync").unique().expect({ timeout: 30_000 });
    await locate(terminal, /Name:\s+alpha-skill/).unique().expect();
    await locate(terminal, /Status:\s+enabled \(pending sync\)/).unique().expect();
    await locate(terminal, "/ alpha-skill  1 match").unique().expect();
    await locate(terminal, "verification pending").expect({ not: true });
    await locate(terminal, "verifying resources").expect({ not: true });
  });
  unchanged();
  await step("clear the verified optional-row filter", () => terminal.write("\x1b"), async () => {
    await locate(terminal, "esc clear").expect({ not: true });
    await locate(terminal, "org-review").unique().expect();
  });
  await select("org-review", async () => {
    await locate(terminal, /Name:\s+org-review/).unique().expect();
    await locate(terminal, /Status:\s+required \(org\)/).unique().expect();
    await locate(terminal, /\sR\s+skill\s+[^\n]*\/org-review/).unique().expect();
    await locate(terminal, /verification\s+pending/).expect({ not: true });
  });
  await step("clear the verified pin filter", () => terminal.write("\x1b"), async () => {
    await locate(terminal, "esc clear").expect({ not: true });
    await locate(terminal, "alpha-skill").unique().expect();
  });
  await select("alpha-skill", async () => {
    await locate(terminal, /Name:\s+alpha-skill/).unique().expect();
    await locate(terminal, /Status:\s+enabled \(pending sync\)/).unique().expect();
  });
  await step("the same verified optional row now opens its toggle", () => terminal.write(" "), async () => {
    await locate(terminal, "toggle global").unique().expect();
    await locate(terminal, "toggle project").unique().expect();
  });
  await step("cancel without changing the selection", () => terminal.write("\x1b"), async () => {
    await locate(terminal, "toggle global").expect({ not: true });
    await locate(terminal, /Name:\s+alpha-skill/).unique().expect();
  });
  unchanged();
  await step("sync becomes eligible and materializes verified selections", () => terminal.write("s"), async () => {
    await locate(terminal, "~ in sync").unique().expect({ timeout: 30_000 });
    await locate(terminal, /Status:\s+enabled/).unique().expect();
  });
  for (const harness of [".factory", ".gemini", ".config/devin"]) {
    for (const [name, expected] of [["alpha-skill", fixture.expectedAlphaSkillContent], ["beta-skill", fixture.expectedBetaSkillContent]]) {
      assertContent(path.join(fixture.home, harness, "skills", name, "SKILL.md"), expected);
    }
  }
  assert.ok(fixture.expectedOrgSkillContent);
  assertContent(path.join(fixture.home, ".factory", "skills", "org-review", "SKILL.md"), fixture.expectedOrgSkillContent);
  for (const harness of [".factory", ".gemini", ".devin"]) {
    assertContent(path.join(fixture.repo, harness, "skills", "project-skill", "SKILL.md"),
      "---\nname: project-skill\ndescription: Project startup control\n---\n\nProject control.\n");
    for (const name of ["committed-hidden", "personal-hidden"]) {
      assert.equal(fs.lstatSync(path.join(fixture.repo, harness, "skills", name), { throwIfNoEntry: false }), undefined, name);
    }
  }
  await step("quit after synchronization", () => terminal.write("q"), async () => {
    assert.equal((await waitForExit(terminal)).exitCode, 0);
  });
  assert.deepEqual(snapshot(configuredFiles), baseline);
  assert.equal(fixture.marketplaceAcquisitionDataRequestCount(), 4, "startup and sync must reuse transient evidence");
});

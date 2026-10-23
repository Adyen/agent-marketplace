import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { createHash } from "node:crypto";
import { after, before } from "node:test";

import { readRegularFile } from "./process-helpers.js";

import {
  clipboardWrites,
  defineFixture,
  findTextRun,
  locate,
  sgr,
  sgrDrag as drag,
  step,
  waitForExit,
  waitForInverse
} from "./helpers.js";

const journey = defineFixture("marketplace-full-view", ["tui"], 80, 24, { organization: "published-lock" });
before(journey.setup);
after(journey.cleanup);

// tui-test locators may read the second cell of a wide character as a space.
const unicodeMarker = /αβ日 ?本 ?語/;

const treeDigest = (root: string): Map<string, string> => {
  const snapshot = new Map<string, string>();
  const walk = (dir: string): void => {
    if (!fs.existsSync(dir)) return;
    for (const entry of fs.readdirSync(dir, { withFileTypes: true })) {
      const full = path.join(dir, entry.name);
      const stat = fs.lstatSync(full);
      if (stat.isSymbolicLink()) {
        snapshot.set(full, `link:${fs.readlinkSync(full)}`);
      } else if (stat.isDirectory()) {
        walk(full);
      } else if (stat.isFile()) {
        snapshot.set(full, `file:${createHash("sha256").update(readRegularFile(full)).digest("hex")}`);
      }
    }
  };
  walk(root);
  return snapshot;
};

// spec: tui-navigation/inspect-an-exact-skill-before-installation-without-writes
// spec: tui-mouse-navigation/bundle-tree-and-preview-receive-their-own-wheel-input
// spec: tui-mouse-navigation/marketplace-selection-and-scrolling-preserve-clipboard-boundaries
journey.test("marketplace full view browses inspection content, selects preview text, and keeps wheel routing", async (terminal) => {
  const fixture = journey.current();
  const controlledPaths = [
    path.join(fixture.home, ".agents"),
    path.join(fixture.home, ".factory"),
    path.join(fixture.home, ".claude"),
    fixture.repo,
    path.dirname(fixture.orgConfigPath!)
  ];
  const baseline = new Map(controlledPaths.map((root) => [root, treeDigest(root)]));

  const longName = "pack-notes-with-a-much-longer-catalog-row-label";
  await step("settled baseline without the discovery row", () => {}, async () => {
    await locate(terminal, "org-review").expect();
    await locate(terminal, longName).expect({ not: true });
    await locate(terminal, "~ in sync").unique().expect();
  });

  await step("search discovers the uninstalled marketplace skill", () => terminal.write("/pack-notes\r"), async () => {
    await locate(terminal, "org-market/tools/pack-notes-with").expect();
    await locate(terminal, "-a-much-longer-catalog-row-label").expect();
    await locate(terminal, /available via Marketplace/).unique().expect();
  });

  await step("open full view loads the verified inspection", () => terminal.write("\r"), async () => {
    await locate(terminal, "Remote inspection primary copy.").expect();
    await locate(terminal, "SKILL.md").expect();
    await locate(terminal, "notes.md").expect();
  });

  await step("support file previews lazily fetched content", () => terminal.write("k"), async () => {
    await locate(terminal, "Support line 01").expect();
    await locate(terminal, "Support line 40").expect({ not: true });
  });

  await step("wheel down in the preview scrolls past the first page", () => terminal.write(sgr(65, 60, 8, "M").repeat(80)), async () => {
    await locate(terminal, "Support line 40").expect();
    await locate(terminal, "Support line 01").expect({ not: true });
  });

  await step("tree wheel switches to the primary file and its preview", () => terminal.write(sgr(65, 2, 3, "M")), async () => {
    await locate(terminal, "Remote inspection primary copy.").expect();
    await locate(terminal, "Support line 40").expect({ not: true });
  });

  await step("drag across the unicode preview text highlights and copies exactly", async () => {
    const run = await findTextRun(terminal, unicodeMarker);
    await terminal.write(drag(run.x, run.y, run.x + run.width - 1, run.y));
  }, async () => {
    await waitForInverse(terminal, unicodeMarker, 1);
    await terminal.waitClipboard("αβ日本語");
  });

  await step("wheel scroll still routes and clears the selection", () => terminal.write(sgr(65, 60, 8, "M")), async () => {
    await waitForInverse(terminal, unicodeMarker, 0);
    assert.equal(await terminal.getClipboard(), "αβ日本語");
  });

  await step("escape leaves the full view", () => terminal.write("\x1b"), async () => {
    await locate(terminal, "Remote inspection primary copy.").expect({ not: true });
    await locate(terminal, /available via Marketplace/).unique().expect();
  });

  await step("ctrl+c exits", () => terminal.write("\x03"), async () => {
    assert.equal((await waitForExit(terminal, 5_000)).exitCode, 0);
  });
  assert.deepEqual(await clipboardWrites(terminal), ["αβ日本語"]);

  for (const [root, snapshot] of baseline) {
    const current = treeDigest(root);
    const diff = [...current.entries()].filter(([file, digest]) => snapshot.get(file) !== digest);
    const removed = [...snapshot.keys()].filter((file) => !current.has(file));
    assert.deepEqual(
      diff.map(([file]) => file),
      [],
      `inspection mutated fixture files under ${root}`
    );
    assert.deepEqual(removed, [], `inspection removed fixture files under ${root}`);
  }
});

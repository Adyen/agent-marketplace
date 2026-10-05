import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { createHash } from "node:crypto";

import { expect, test } from "@microsoft/tui-test";

import {
  declineLegacyMigration,
  defineFixture,
  findTextRun,
  osc52Capture,
  sgr,
  sgrDrag as drag,
  step,
  waitForExit,
  waitForInverse
} from "./helpers.js";

const journey = defineFixture("marketplace-full-view", ["tui"], 80, 24, { organization: "published-lock" });
test.use(journey.options);
test.beforeAll(journey.setup);
test.afterAll(journey.cleanup);

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
        snapshot.set(full, `file:${createHash("sha256").update(fs.readFileSync(full)).digest("hex")}`);
      }
    }
  };
  walk(root);
  return snapshot;
};

test("marketplace full view browses inspection content, selects preview text, and keeps wheel routing", async ({
  terminal
}) => {
  const fixture = journey.current();
  const controlledPaths = [
    path.join(fixture.home, ".agents"),
    path.join(fixture.home, ".factory"),
    path.join(fixture.home, ".claude"),
    fixture.repo,
    path.dirname(fixture.orgConfigPath!)
  ];
  const before = new Map(controlledPaths.map((root) => [root, treeDigest(root)]));

  const osc52 = osc52Capture(terminal);
  await declineLegacyMigration(terminal);

  const longName = "pack-notes-with-a-much-longer-catalog-row-label";
  await step("settled baseline without the discovery row", () => {}, async () => {
    await expect(terminal.getByText("org-review", { strict: false })).toBeVisible();
    await expect(terminal.getByText(longName, { strict: false })).not.toBeVisible();
  });

  await step("search discovers the uninstalled marketplace skill", () => terminal.write("/pack-notes\r"), async () => {
    await expect(terminal.getByText("org-market/tools/pack-notes-with", { strict: false })).toBeVisible();
    await expect(terminal.getByText("-a-much-longer-catalog-row-label", { strict: false })).toBeVisible();
    await expect(terminal.getByText(/available via Marketplace/g)).toBeVisible();
  });

  await step("open full view loads the verified inspection", () => terminal.write("\r"), async () => {
    await expect(terminal.getByText("Remote inspection primary copy.", { strict: false })).toBeVisible();
    await expect(terminal.getByText("SKILL.md", { strict: false })).toBeVisible();
    await expect(terminal.getByText("notes.md", { strict: false })).toBeVisible();
  });

  await step("support file previews lazily fetched content", () => terminal.write("k"), async () => {
    await expect(terminal.getByText("Support line 01", { strict: false })).toBeVisible();
    await expect(terminal.getByText("Support line 40", { strict: false })).not.toBeVisible();
  });

  await step("wheel down in the preview scrolls past the first page", () => terminal.write(sgr(65, 60, 8, "M").repeat(80)), async () => {
    await expect(terminal.getByText("Support line 40", { strict: false })).toBeVisible();
    await expect(terminal.getByText("Support line 01", { strict: false })).not.toBeVisible();
  });

  await step("tree wheel switches to the primary file and its preview", () => terminal.write(sgr(65, 2, 3, "M")), async () => {
    await expect(terminal.getByText("Remote inspection primary copy.", { strict: false })).toBeVisible();
    await expect(terminal.getByText("Support line 40", { strict: false })).not.toBeVisible();
  });

  let dragSent = "";
  await step("drag across the unicode preview text highlights and copies exactly", async () => {
    const run = findTextRun(terminal, /αβ日本語/g);
    assert.ok(run, "preview does not show the unicode marker");
    dragSent = drag(run.x, run.y, run.x + run.cells.length - 1, run.y);
    await terminal.write(dragSent);
  }, async () => {
    await waitForInverse(terminal, /αβ日本語/g, 1);
    const payloads = osc52.payloads();
    assert.equal(payloads.length, 1);
    assert.equal(Buffer.from(payloads[0], "base64").toString(), "αβ日本語");
  });

  await step("wheel scroll still routes and clears the selection", () => terminal.write(sgr(65, 60, 8, "M")), async () => {
    await waitForInverse(terminal, /αβ日本語/g, 0);
    assert.equal(osc52.payloads().length, 1);
  });

  await step("escape leaves the full view", () => terminal.write("\x1b"), async () => {
    await expect(terminal.getByText("Remote inspection primary copy.", { strict: false })).not.toBeVisible();
    await expect(terminal.getByText(/available via Marketplace/g)).toBeVisible();
  });

  await step("ctrl+c exits", () => terminal.write("\x03"), async () => {
    assert.equal((await waitForExit(terminal, 5_000)).exitCode, 0);
  });

  for (const [root, snapshot] of before) {
    const after = treeDigest(root);
    const diff = [...after.entries()].filter(([file, digest]) => snapshot.get(file) !== digest);
    const removed = [...snapshot.keys()].filter((file) => !after.has(file));
    assert.deepEqual(
      diff.map(([file]) => file),
      [],
      `inspection mutated fixture files under ${root}`
    );
    assert.deepEqual(removed, [], `inspection removed fixture files under ${root}`);
  }
});

import assert from "node:assert/strict";

import { expect, test } from "@microsoft/tui-test";

import { defineFixture, step, waitForExit } from "./helpers.js";

const journey = defineFixture("bundle-preview-resize", ["tui"], 80, 24, {
  bundleFiles: {
    "guide.txt": Array.from({ length: 80 }, (_, index) => `Guide line ${String(index + 1).padStart(2, "0")}\n`).join("")
  }
});
test.use(journey.options);
test.beforeAll(journey.setup);
test.afterAll(journey.cleanup);

test("preview scrolling survives pager and resize while short terminals navigate the tree", async ({ terminal }) => {
  await expect(terminal.getByText("~ in sync")).toBeVisible();
  await terminal.write("G");
  await expect(terminal.getByText("guide.txt", { strict: false })).toBeVisible();
  await terminal.write("\r");
  await expect(terminal.getByText("Preview · SKILL.md", { strict: false })).toBeVisible();
  await step("select long plain-text file", () => terminal.write("G"), async () => {
    await expect(terminal.getByText("Preview · guide.txt", { strict: false })).toBeVisible();
    await expect(terminal.getByText("Guide line 01")).toBeVisible();
    await expect(terminal.getByText("Guide line 10")).not.toBeVisible();
  });
  await step("page down scrolls preview, not selection", () => terminal.write("\u001b[6~"), async () => {
    await expect(terminal.getByText("Preview · guide.txt", { strict: false })).toBeVisible();
    await expect(terminal.getByText("Guide line 10")).toBeVisible();
    await expect(terminal.getByText("Guide line 01")).not.toBeVisible();
  });
  await step("pager receives full file", () => terminal.write("\r"), async () => {
    await expect(terminal.getByText("Guide line 01")).toBeVisible();
    await expect(terminal.getByText("Preview · guide.txt", { strict: false })).not.toBeVisible();
  });
  await terminal.write("G");
  await expect(terminal.getByText("Guide line 80")).toBeVisible();
  await step("pager return preserves preview scroll", () => terminal.write("q"), async () => {
    await expect(terminal.getByText("Preview · guide.txt", { strict: false })).toBeVisible();
    await expect(terminal.getByText("Guide line 10")).toBeVisible();
    await expect(terminal.getByText("Guide line 01")).not.toBeVisible();
  });
  await step("narrow resize keeps selected preview and anchor", () => terminal.resize(50, 24), async () => {
    await expect(terminal.getByText("Preview · guide.txt", { strict: false })).toBeVisible();
    await expect(terminal.getByText("Guide line 10")).toBeVisible();
    await expect(terminal.getByText("Guide line 01")).not.toBeVisible();
  });
  await step("short supported terminal hides preview", () => terminal.resize(80, 12), async () => {
    await expect(terminal.getByText("> Files", { strict: false })).toBeVisible();
    await expect(terminal.getByText("Preview", { strict: false })).not.toBeVisible();
    await expect(terminal.getByText("guide.txt", { strict: false })).toBeVisible();
  });
  await terminal.write("\u001b[5~");
  await step("page up without preview selects first tree file", () => terminal.resize(80, 24), async () => {
    await expect(terminal.getByText("Preview · SKILL.md", { strict: false })).toBeVisible();
  });
  await step("switch back resets file preview to top", () => terminal.write("j"), async () => {
    await expect(terminal.getByText("Preview · guide.txt", { strict: false })).toBeVisible();
    await expect(terminal.getByText("Guide line 01")).toBeVisible();
  });
  await step("quit", () => terminal.write("q"), async () => {
    assert.equal((await waitForExit(terminal)).exitCode, 0);
  });
});

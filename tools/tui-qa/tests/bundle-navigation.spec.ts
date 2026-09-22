import assert from "node:assert/strict";

import { expect, test } from "@microsoft/tui-test";

import { declineLegacyMigration, defineFixture, step, waitForExit } from "./helpers.js";

const journey = defineFixture("bundle-navigation", ["tui"], 80, 24, {
  bundleFiles: {
    "docs/nested.txt": "Nested bundle contents.\n",
    "z-last.txt": "Last bundle contents.\n"
  }
});
test.use(journey.options);
test.beforeAll(journey.setup);
test.afterAll(journey.cleanup);

test("nested bundle navigation retains focus, expansion and selection through pager return", async ({ terminal }) => {
  await declineLegacyMigration(terminal);
  await expect(terminal.getByText("~ in sync")).toBeVisible();
  await step("select alpha with inline tree but no preview", () => terminal.write("G"), async () => {
    await expect(terminal.getByText(/Name:\s+alpha-skill/g)).toBeVisible();
    await expect(terminal.getByText("z-last.txt", { strict: false })).toBeVisible();
    await expect(terminal.getByText("Preview", { strict: false })).not.toBeVisible();
  });
  await step("right focuses initially selected SKILL.md", () => terminal.write("\u001b[C"), async () => {
    await expect(terminal.getByText("> Files", { strict: false })).toBeVisible();
    await expect(terminal.getByText("Preview · SKILL.md", { strict: false })).toBeVisible();
  });
  await step("home selects directory and clears preview", () => terminal.write("g"), async () => {
    await expect(terminal.getByText("Select a file to preview")).toBeVisible();
  });
  await step("enter expands directory without opening pager", () => terminal.write("\r"), async () => {
    await expect(terminal.getByText("nested.txt", { strict: false })).toBeVisible();
    await expect(terminal.getByText("Select a file to preview")).toBeVisible();
  });
  await step("down previews nested file", () => terminal.write("j"), async () => {
    await expect(terminal.getByText("Preview · docs/nested.txt", { strict: false })).toBeVisible();
    await expect(terminal.getByText("Nested bundle contents.")).toBeVisible();
  });
  await step("right on file stays in browser", () => terminal.write("\u001b[C"), async () => {
    await expect(terminal.getByText("> Files", { strict: false })).toBeVisible();
    await expect(terminal.getByText("Preview · docs/nested.txt", { strict: false })).toBeVisible();
  });
  await step("enter opens selected nested file in pager", () => terminal.write("\r"), async () => {
    await expect(terminal.getByText("Nested bundle contents.")).toBeVisible();
    await expect(terminal.getByText("(END)", { strict: false })).toBeVisible();
    await expect(terminal.getByText("> Files", { strict: false })).not.toBeVisible();
  });
  await step("pager return restores nested selection", () => terminal.write("q"), async () => {
    await expect(terminal.getByText("> Files", { strict: false })).toBeVisible();
    await expect(terminal.getByText("Preview · docs/nested.txt", { strict: false })).toBeVisible();
  });
  await step("h moves to parent", () => terminal.write("h"), async () => {
    await expect(terminal.getByText("Select a file to preview")).toBeVisible();
    await expect(terminal.getByText("nested.txt", { strict: false })).toBeVisible();
  });
  await step("h folds parent directory", () => terminal.write("h"), async () => {
    await expect(terminal.getByText("nested.txt", { strict: false })).not.toBeVisible();
    await expect(terminal.getByText("Select a file to preview")).toBeVisible();
  });
  await step("left returns to resources even on a directory", () => terminal.write("\u001b[D"), async () => {
    await expect(terminal.getByText("> Files", { strict: false })).not.toBeVisible();
    await expect(terminal.getByText(/Name:\s+alpha-skill/g)).toBeVisible();
  });
  await step("refocus retains folded directory selection", () => terminal.write("\r"), async () => {
    await expect(terminal.getByText("Select a file to preview")).toBeVisible();
    await expect(terminal.getByText("nested.txt", { strict: false })).not.toBeVisible();
  });
  await step("end selects last file", () => terminal.write("G"), async () => {
    await expect(terminal.getByText("Preview · z-last.txt", { strict: false })).toBeVisible();
    await expect(terminal.getByText("Last bundle contents.")).toBeVisible();
  });
  await step("escape and refocus retain file selection", () => terminal.write("\u001b"), async () => {
    await expect(terminal.getByText("> Files", { strict: false })).not.toBeVisible();
  });
  await terminal.write("\r");
  await expect(terminal.getByText("Preview · z-last.txt", { strict: false })).toBeVisible();
  await terminal.write("\u001b[D");
  await expect(terminal.getByText("> Files", { strict: false })).not.toBeVisible();
  await terminal.write("g");
  await step("select beta resource and replace alpha tree", async () => {
    for (let index = 0; index < 3; index++) await terminal.write("\u001b[B");
  }, async () => {
    await expect(terminal.getByText(/Name:\s+beta-skill/g)).toBeVisible();
    await expect(terminal.getByText("SKILL.md", { strict: false })).toBeVisible();
    await expect(terminal.getByText("z-last.txt", { strict: false })).not.toBeVisible();
    await expect(terminal.getByText("docs", { strict: false })).not.toBeVisible();
  });
  await step("focus beta preview without stale alpha content", () => terminal.write("\r"), async () => {
    await expect(terminal.getByText("Preview · SKILL.md", { strict: false })).toBeVisible();
    await expect(terminal.getByText("Last bundle contents.")).not.toBeVisible();
  });
  await step("q quits directly from file pane", () => terminal.write("q"), async () => {
    assert.equal((await waitForExit(terminal)).exitCode, 0);
  });
});

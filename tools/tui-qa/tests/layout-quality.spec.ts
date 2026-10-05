import assert from "node:assert/strict";

import { expect, test } from "@microsoft/tui-test";

import { declineLegacyMigration, defineFixture, step, waitForExit } from "./helpers.js";

const journey = defineFixture("layout-quality", ["tui"]);
test.use(journey.options);
test.beforeAll(journey.setup);
test.afterAll(journey.cleanup);

const frameTop = /╭─+╮/g;
const frameBottom = /╰─+╯/g;
const footerFloor = /: (commands|cmds) · \? help · q quit/g;

// spec: tui-rendering/modal-at-common-sizes
test("footer, palette and modals stay framed and discoverable", async ({ terminal }) => {
  await declineLegacyMigration(terminal);
  await step("footer keeps the palette, help and quit hints", () => {}, async () => {
    await expect(terminal.getByText("beta-skill", { strict: false })).toBeVisible();
    await expect(terminal.getByText(footerFloor)).toBeVisible();
    await expect(terminal.getByText("/ search", { strict: false })).toBeVisible();
  });

  await step("toggle modal is one bounded frame", () => terminal.write("G "), async () => {
    await expect(terminal.getByText("toggle local", { strict: false })).toBeVisible();
    await expect(terminal.getByText(frameTop)).toBeVisible();
    await expect(terminal.getByText(frameBottom)).toBeVisible();
    await expect(terminal.getByText("enter apply · esc cancel", { strict: false })).toBeVisible();
    await expect(terminal.getByText("┃", { strict: false })).not.toBeVisible();
  });

  await step("close the toggle modal", () => terminal.write("\x1b"), async () => {
    await expect(terminal.getByText("toggle local", { strict: false })).not.toBeVisible();
  });

  await step("edit-config modal keeps file names", () => terminal.write("E"), async () => {
    await expect(terminal.getByText("Edit Config", { strict: false })).toBeVisible();
    await expect(terminal.getByText("config.local.yaml", { strict: false })).toBeVisible();
    await expect(terminal.getByText(frameBottom)).toBeVisible();
    await expect(terminal.getByText("enter open · esc cancel", { strict: false })).toBeVisible();
  });

  await step("close the edit-config modal", () => terminal.write("\x1b"), async () => {
    await expect(terminal.getByText("Edit Config", { strict: false })).not.toBeVisible();
  });

  await step("minimum-size footer", () => terminal.resize(50, 12), async () => {
    await expect(terminal.getByText(footerFloor)).toBeVisible();
  });

  await step("palette fits the minimum size", () => terminal.write(":"), async () => {
    await expect(terminal.getByText("Command palette", { strict: false })).toBeVisible();
    await expect(terminal.getByText("enter run", { strict: false })).toBeVisible();
    await expect(terminal.getByText(footerFloor)).toBeVisible();
  });

  await step("close the palette", () => terminal.write("\x1b"), async () => {
    await expect(terminal.getByText("Command palette", { strict: false })).not.toBeVisible();
  });

  await step("quit", () => terminal.write("q"), async () => {
    assert.equal((await waitForExit(terminal)).exitCode, 0);
  });
});

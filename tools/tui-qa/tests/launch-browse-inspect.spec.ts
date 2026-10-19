import assert from "node:assert/strict";

import { expect, test } from "@microsoft/tui-test";

import { defineFixture, step, waitForExit } from "./helpers.js";

const journey = defineFixture("launch-browse-inspect", []);
test.use(journey.options);
test.beforeAll(journey.setup);
test.afterAll(journey.cleanup);

test("bare launch browses the single status view and inspects alpha-skill", async ({ terminal }) => {
  await step("settled fixture baseline", () => {}, async () => {
    await expect(terminal.getByText("beta-skill", { strict: false })).toBeVisible();
    await expect(terminal.getByText("alpha-skill", { strict: false })).toBeVisible();
    await expect(terminal.getByText("~ in sync")).toBeVisible();
    await expect(terminal.getByText(/Enabled\s+\(1\)/g)).toBeVisible();
    await expect(terminal.getByText(/Available\s+\(1\)/g)).toBeVisible();
  });

  await step("former tab keys leave the list unchanged", () => terminal.write("123"), async () => {
    await expect(terminal.getByText(/Enabled\s+\(1\)/g)).toBeVisible();
    await expect(terminal.getByText(/Available\s+\(1\)/g)).toBeVisible();
    await expect(terminal.getByText(/1 Scope\s+2 Registry\s+3 Status/g)).not.toBeVisible();
  });

  await step("search narrows the single list", () => terminal.write("/alpha\r"), async () => {
    await expect(terminal.getByText(/Name:\s+alpha-skill/g)).toBeVisible();
  });

  await step("clear the search", () => terminal.write("\u001b"), async () => {
    await expect(terminal.getByText(/Enabled\s+\(1\)/g)).toBeVisible();
    await expect(terminal.getByText(/Available\s+\(1\)/g)).toBeVisible();
  });

  await step("fold and expand the Enabled section", () => terminal.write("gh"), async () => {
    await expect(terminal.getByText(/▶ Enabled\s+\(1\)/g)).toBeVisible();
    await terminal.write("l");
    await expect(terminal.getByText(/▼ Enabled\s+\(1\)/g)).toBeVisible();
  });

  await step("select available alpha-skill", () => terminal.write("G"), async () => {
    await expect(terminal.getByText(/Name:\s+alpha-skill/g)).toBeVisible();
    await expect(terminal.getByText(/Status:\s+available/g)).toBeVisible();
  });

  await step("open full alpha-skill detail", () => terminal.write("\r"), async () => {
    await expect(terminal.getByText("Alpha Skill", { strict: false })).toBeVisible();
    await expect(terminal.getByText("Alpha fixture body.")).toBeVisible();
  });

  await step("return to selected alpha-skill", () => terminal.write("\u001b"), async () => {
    await expect(terminal.getByText(/Enabled\s+\(1\)/g)).toBeVisible();
    await expect(terminal.getByText(/1 Scope\s+2 Registry\s+3 Status/g)).not.toBeVisible();
    await expect(terminal.getByText(/Name:\s+alpha-skill/g)).toBeVisible();
  });

  await step("quit", () => terminal.write("q"), async () => {
    assert.equal((await waitForExit(terminal)).exitCode, 0);
  });
});

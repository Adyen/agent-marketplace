import assert from "node:assert/strict";

import { expect, test } from "@microsoft/tui-test";

import { declineLegacyMigration, defineFixture, step, waitForExit } from "./helpers.js";

const journey = defineFixture("launch-browse-inspect", []);
test.use(journey.options);
test.beforeAll(journey.setup);
test.afterAll(journey.cleanup);

test("bare launch browses tabs and inspects alpha-skill", async ({ terminal }) => {
  await declineLegacyMigration(terminal);
  await step("settled fixture baseline", () => {}, async () => {
    await expect(terminal.getByText("beta-skill", { strict: false })).toBeVisible();
    await expect(terminal.getByText("alpha-skill", { strict: false })).toBeVisible();
    await expect(terminal.getByText("~ in sync")).toBeVisible();
  });

  await step("open Registry tab", () => terminal.write("2"), async () => {
    await expect(terminal.getByText(/fixture \/ skills\s+\(2\)/g)).toBeVisible();
  });

  await step("open Status tab", () => terminal.write("3"), async () => {
    await expect(terminal.getByText(/Enabled\s+\(1\)/g)).toBeVisible();
    await expect(terminal.getByText(/Available\s+\(1\)/g)).toBeVisible();
  });

  await step("return to Scope tab", () => terminal.write("1"), async () => {
    await expect(terminal.getByText(/Project\s+\(1\)/g)).toBeVisible();
    await expect(terminal.getByText(/Available\s+\(1\)/g)).toBeVisible();
  });

  await step("select available alpha-skill", () => terminal.write("G"), async () => {
    await expect(terminal.getByText(/Name:\s+alpha-skill/g)).toBeVisible();
    await expect(terminal.getByText(/Status:\s+available/g)).toBeVisible();
  });

  await step("focus alpha-skill files", () => terminal.write("\r"), async () => {
    await expect(terminal.getByText("Preview · SKILL.md", { strict: false })).toBeVisible();
  });

  await step("open full alpha-skill detail", () => terminal.write("\r"), async () => {
    await expect(terminal.getByText("Alpha Skill", { strict: false })).toBeVisible();
    await expect(terminal.getByText("Alpha fixture body.")).toBeVisible();
  });

  await step("close alpha-skill pager", () => terminal.write("q"), async () => {
    await expect(terminal.getByText("Preview · SKILL.md", { strict: false })).toBeVisible();
  });

  await step("return to selected alpha-skill", () => terminal.write("\u001b"), async () => {
    await expect(terminal.getByText(/1 Scope\s+2 Registry\s+3 Status/g)).toBeVisible();
    await expect(terminal.getByText(/Name:\s+alpha-skill/g)).toBeVisible();
  });

  await step("quit", () => terminal.write("q"), async () => {
    assert.equal((await waitForExit(terminal)).exitCode, 0);
  });
});

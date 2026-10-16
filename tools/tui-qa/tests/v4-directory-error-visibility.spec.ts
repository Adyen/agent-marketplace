import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";

import { expect, test } from "@microsoft/tui-test";

import { defineFixture, step, waitForExit } from "./helpers.js";

const journey = defineFixture("v4-directory-error-visibility", ["tui"], 80, 24, { directorySource: true });
test.use(journey.options);
test.beforeAll(journey.setup);
test.afterAll(journey.cleanup);

test("keeps the v4 mutation error visible at narrow widths", async ({ terminal }) => {
  const fixture = journey.current();
  const globalConfig = path.join(fixture.home, ".agents", "config.yaml");
  await step("healthy directory startup is visible", () => {}, async () => {
    await expect(terminal.getByText("alpha-skill", { strict: false })).toBeVisible();
    await expect(terminal.getByText("beta-skill", { strict: false })).toBeVisible();
    await expect(terminal.getByText("~ in sync")).toBeVisible();
  });
  await step("open search", () => terminal.write("/"), async () => {
    await expect(terminal.getByText(/\/\s{20,}/g)).toBeVisible();
  });
  await step("filter for alpha", () => terminal.write("alpha"), async () => {
    await expect(terminal.getByText("alpha-skill", { strict: false })).toBeVisible();
  });
  await step("apply alpha filter", () => terminal.write("\r"), async () => {
    await expect(terminal.getByText(/Name:\s+alpha-skill/g)).toBeVisible();
  });
  await step("open toggle before invalidating config", () => terminal.write(" "), async () => {
    await expect(terminal.getByText("toggle global")).toBeVisible();
  });
  const invalidConfig = "apiVersion: agent-marketplace/v4\nunknown_field: true\n";
  await step("invalidate global config", () => fs.writeFileSync(globalConfig, invalidConfig, { encoding: "utf8", mode: 0o640 }), async () => {
    await expect(terminal.getByText("toggle global")).toBeVisible();
  });
  await step("failed global toggle retains diagnostic", () => terminal.write("g"), async () => {
    await expect(terminal.getByText(/! 1 errors/g)).toBeVisible();
    await expect(terminal.getByText("e errors", { strict: false })).toBeVisible();
  });
  await step("narrow error footer keeps recovery actions", () => terminal.resize(50, 12), async () => {
    await expect(terminal.getByText(/! 1 errors/g)).toBeVisible();
    await expect(terminal.getByText("e errors", { strict: false })).toBeVisible();
    await expect(terminal.getByText("r reload", { strict: false })).toBeVisible();
    await expect(terminal.getByText("q quit", { strict: false })).toBeVisible();
  });
  // The diagnostic names the authored file, so at 50x12 the message wraps at a
  // path-dependent position and a long path can push the trailing hint below
  // the viewport; assert on the title, a token wrapping cannot split, and the
  // footer row that is always on screen.
  await step("open complete error diagnostic", () => terminal.write("e"), async () => {
    await expect(terminal.getByText("Errors (1)")).toBeVisible();
    await expect(terminal.getByText("unknown_field", { strict: false })).toBeVisible();
    await expect(terminal.getByText("esc/e close", { strict: false })).toBeVisible();
  });
  assert.equal(fs.readFileSync(globalConfig, "utf8"), invalidConfig);
  await step("close error diagnostic", () => terminal.write("e"), async () => {
    await expect(terminal.getByText("unknown_field", { strict: false })).not.toBeVisible();
  });
  await step("quit after error", () => terminal.write("q"), async () => {
    assert.equal((await waitForExit(terminal)).exitCode, 0);
  });
});

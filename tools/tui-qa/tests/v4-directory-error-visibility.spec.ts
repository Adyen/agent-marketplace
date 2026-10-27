import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { after, before } from "node:test";

import { defineFixture, locate, step, waitForExit } from "./helpers.js";

const journey = defineFixture("v4-directory-error-visibility", ["tui"], 80, 24, { directorySource: true });
before(journey.setup);
after(journey.cleanup);

journey.test("keeps the v4 mutation error visible at narrow widths", async (terminal) => {
  const fixture = journey.current();
  const globalConfig = path.join(fixture.home, ".agents", "config.yaml");
  await step("healthy directory startup is visible", () => {}, async () => {
    await locate(terminal, "alpha-skill").expect();
    await locate(terminal, "beta-skill").expect();
    await locate(terminal, "~ in sync").unique().expect();
  });
  await step("open search", () => terminal.write("/"), async () => {
    await locate(terminal, /\/\s{20,}/).unique().expect();
  });
  await step("filter for alpha", () => terminal.write("alpha"), async () => {
    await locate(terminal, "alpha-skill").expect();
  });
  await step("apply alpha filter", () => terminal.write("\r"), async () => {
    await locate(terminal, /Name:\s+alpha-skill/).unique().expect();
  });
  await step("open toggle before invalidating config", () => terminal.write(" "), async () => {
    await locate(terminal, "toggle global").unique().expect();
  });
  const invalidConfig = "apiVersion: agent-marketplace/v4\nunknown_field: true\n";
  await step("invalidate global config", () => fs.writeFileSync(globalConfig, invalidConfig, { encoding: "utf8", mode: 0o640 }), async () => {
    await locate(terminal, "toggle global").unique().expect();
  });
  await step("failed global toggle retains diagnostic", () => terminal.write("g"), async () => {
    await locate(terminal, /! 1 errors/).unique().expect();
    await locate(terminal, "e errors").expect();
  });
  await step("catalog remediation is visible in the diagnostic", () => terminal.write("e"), async () => {
    await locate(terminal, "unsupported").expect();
  });
  await step("close diagnostic before narrowing", () => terminal.write("e"), async () => {
    await locate(terminal, "e errors").expect();
  });
  await step("narrow error footer keeps recovery actions", () => terminal.resize(50, 12), async () => {
    await locate(terminal, /! 1 errors/).unique().expect();
    await locate(terminal, "e errors").expect();
    await locate(terminal, "r reload").expect();
    await locate(terminal, "q quit").expect();
  });
  // The diagnostic names the authored file, so at 50x12 the message wraps at a
  // path-dependent position and a long path can push the trailing hint below
  // the viewport; assert on the title, a token wrapping cannot split, and the
  // footer row that is always on screen.
  await step("open complete error diagnostic", () => terminal.write("e"), async () => {
    await locate(terminal, "Errors (1)").unique().expect();
    await locate(terminal, "unknown_field").expect();
    await locate(terminal, "top-level").expect();
    await locate(terminal, "esc/e close").expect();
  });
  assert.equal(fs.readFileSync(globalConfig, "utf8"), invalidConfig);
  await step("close error diagnostic", () => terminal.write("e"), async () => {
    await locate(terminal, "unknown_field").expect({ not: true });
  });
  await step("quit after error", () => terminal.write("q"), async () => {
    assert.equal((await waitForExit(terminal)).exitCode, 0);
  });
});

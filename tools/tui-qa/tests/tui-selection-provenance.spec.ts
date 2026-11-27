import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { after, before } from "node:test";

import { defineFixture, locate, step, waitForExit } from "./helpers.js";
import { runAM } from "./process-helpers.js";

// spec: tui-rendering/optional-organization-default-and-collection-selected-resource-show-provenance
const journey = defineFixture("tui-selection-provenance", ["tui"], 80, 24, { collection: true });

before(async () => {
  await journey.setup();
  const fixture = journey.current();
  const organizationConfig = path.join(fixture.root, "etc", "agent-marketplace", "config.yaml");
  fs.mkdirSync(path.dirname(organizationConfig), { recursive: true });
  fs.writeFileSync(
    organizationConfig,
    `apiVersion: agent-marketplace/v4\nsources:\n  org-fixture:\n    git: ${JSON.stringify(fixture.registryURL)}\n    ref: main\n    path: skills\nresources:\n  org-fixture:\n    include:\n      - collection/starter\n`
  );
  fixture.env.AGENT_MARKETPLACE_V3_ORG_CONFIG = organizationConfig;
  fs.writeFileSync(
    path.join(fixture.home, ".agents", "config.yaml"),
    `apiVersion: agent-marketplace/v4\nharnesses: [factory]\nsources:\n  user-fixture:\n    git: ${JSON.stringify(fixture.registryURL)}\n    ref: main\n    path: skills\nresources:\n  user-fixture:\n    include:\n      - skill/beta-skill\n    required: true\n`
  );
  const lock = await runAM(fixture, ["lock", "--scope", "user"]);
  if (lock.status !== 0) throw new Error(`user lock failed:\n${lock.stdout}\n${lock.stderr}`);
});
after(journey.cleanup);

journey.test("optional organization Git collection member shows its selection provenance", async (terminal) => {
  await step("settled organization collection row is available", () => {}, async () => {
    await locate(terminal, "alpha-skill").expect({ timeout: 30_000 });
    await locate(terminal, /~ (in sync|\d+ out of sync)/).unique().expect({ timeout: 30_000 });
  });

  await step("open search", () => terminal.write("/"), async () => {
    await locate(terminal, /\/ {20,}/).unique().expect();
  });

  await step("filter to the collection member", () => terminal.write("alpha-skill"), async () => {
    await locate(terminal, "/ alpha-skill").expect();
    await locate(terminal, "alpha-skill").expect();
  });

  await step("apply the filter and inspect the member", () => terminal.write("\r"), async () => {
    await locate(terminal, /O\s+skill/).unique().expect();
    await locate(terminal, /Source layer:\s+organization default \(optional\)/).unique().expect();
    await locate(terminal, /Included via:\s+starter/).unique().expect();
  });

  await step("clear the collection member filter", () => terminal.write("\x1b"), async () => {
    await locate(terminal, "esc clear").expect({ not: true });
    await locate(terminal, "beta-skill").expect();
  });

  await step("open search for the required user resource", () => terminal.write("/"), async () => {
    await locate(terminal, /\/ {20,}/).unique().expect();
  });

  await step("filter to the required user resource", () => terminal.write("beta-skill"), async () => {
    await locate(terminal, "/ beta-skill").expect();
    await locate(terminal, "beta-skill").expect();
  });

  await step("inspect the required user resource", () => terminal.write("\r"), async () => {
    await locate(terminal, /R\s+skill/).unique().expect();
    await locate(terminal, /Source layer:\s+user, project/).unique().expect();
  });

  await step("confirm pending work before quitting", () => terminal.write("q"), async () => {
    await locate(terminal, "Sync+quit?").unique().expect();
  });

  await step("quit without syncing", () => terminal.write("n"), async () => {
    assert.equal((await waitForExit(terminal)).exitCode, 0);
  });
});

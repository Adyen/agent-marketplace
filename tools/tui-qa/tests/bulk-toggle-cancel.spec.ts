import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { after, before } from "node:test";

import { defineFixture, locate, step, waitForExit } from "./helpers.js";

// spec: tui-interactions/scope-direction-confirmation-and-cancellation
const repoJourney = defineFixture("bulk-toggle-cancel", ["tui"]);
before(repoJourney.setup);
after(repoJourney.cleanup);

// The config files any bulk write could touch, as bytes or absent.
const configSnapshot = (files: string[]): (Buffer | undefined)[] => files.map((file) => (fs.existsSync(file) ? fs.readFileSync(file) : undefined));

repoJourney.test("the section bulk modal writes nothing when cancelled at any stage", async (terminal) => {
  const fixture = repoJourney.current();
  const files = [
    path.join(fixture.repo, ".agents", "config.yaml"),
    path.join(fixture.repo, ".agents", "config.local.yaml"),
    path.join(fixture.home, ".agents", "config.yaml")
  ];
  const before = configSnapshot(files);
  const openModal = async (attempt: string): Promise<void> => {
    await step(`open the Available section's bulk modal (${attempt})`, () => terminal.write(" "), async () => {
      await locate(terminal, "Toggle all in Available").unique().expect();
      await locate(terminal, /1 matching · 1 eligible · 0 skipped/).unique().expect();
    });
  };
  const cancel = async (stage: string): Promise<void> => {
    await step(`esc at the ${stage} stage closes the modal`, () => terminal.write("\x1b"), async () => {
      await locate(terminal, "Toggle all in Available").expect({ not: true });
      await locate(terminal, "Select a resource to see details").unique().expect();
    });
    assert.deepEqual(configSnapshot(files), before, `cancelling at the ${stage} stage wrote config`);
  };

  await step("settled fixture baseline", () => {}, async () => {
    await locate(terminal, "~ in sync").unique().expect({ timeout: 30_000 });
  });
  await step("select alpha-skill", () => terminal.write("G"), async () => {
    await locate(terminal, /Name:\s+alpha-skill/).unique().expect();
  });
  await step("select the Available header", () => terminal.write("k"), async () => {
    await locate(terminal, "Select a resource to see details").unique().expect();
  });

  await openModal("scope");
  await cancel("scope");

  await openModal("direction");
  await step("choose project scope", () => terminal.write("p"), async () => {
    await locate(terminal, /scope: project/).unique().expect();
  });
  await cancel("direction");

  await openModal("confirmation");
  await step("choose project scope again", () => terminal.write("p"), async () => {
    await locate(terminal, /scope: project/).unique().expect();
  });
  await step("choose enable", () => terminal.write("+"), async () => {
    await locate(terminal, /enable 1 resources in the project layer/).unique().expect();
  });
  await cancel("confirmation");

  await step("the list is unchanged", () => {}, async () => {
    await locate(terminal, /Available\s+\(1\)/).unique().expect();
    await locate(terminal, "~ in sync").unique().expect();
  });
  await step("quit", () => terminal.write("q"), async () => {
    assert.equal((await waitForExit(terminal)).exitCode, 0);
  });
});

import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { after, before } from "node:test";

import { defineFixture, locate, step, waitForExit } from "./helpers.js";
import { runNonTTY } from "./process-helpers.js";

const journey = defineFixture("local-toggle-and-sync", ["tui"]);
before(async () => {
  await journey.setup();
  const fixture = journey.current();
  fixture.env.AGENT_MARKETPLACE_LOG_ENABLED = "true";
  fixture.env.AGENT_MARKETPLACE_LOG_LEVEL = "debug";
  fixture.env.AGENT_MARKETPLACE_LOG_FILE = path.join(fixture.root, "source-revision.jsonl");
});
after(journey.cleanup);

journey.test("local toggle reaches pending state before sync", async (terminal) => {
  const fixture = journey.current();
  const generatedSkill = path.join(fixture.repo, ".factory", "skills", "alpha-skill");
  const generatedContent = path.join(generatedSkill, "SKILL.md");
  const localConfig = path.join(fixture.repo, ".agents", "config.local.yaml");

  await step("settled fixture baseline", () => {}, async () => {
    await locate(terminal, "beta-skill").expect();
    await locate(terminal, "alpha-skill").expect();
    await locate(terminal, "~ in sync").unique().expect();
  });
  assert.equal(fs.lstatSync(generatedSkill, { throwIfNoEntry: false }), undefined);

  await step("select available alpha-skill", () => terminal.write("G"), async () => {
    await locate(terminal, /Name:\s+alpha-skill/).unique().expect();
    await locate(terminal, /Status:\s+available/).unique().expect();
  });

  await step("open scope toggle", () => terminal.write(" "), async () => {
    await locate(terminal, '"alpha-skill"').expect();
    await locate(terminal, "toggle local").unique().expect();
  });

  await step("toggle local", () => terminal.write("l"), async () => {
    await locate(terminal, /Pending sync\s+\(1\)/).unique().expect();
    await locate(terminal, /Status:\s+enabled, waiting for sync/).unique().expect();
    await locate(terminal, "~ 1 out of sync").unique().expect();
  });
  assert.equal(fs.readFileSync(localConfig, "utf8"), fixture.expectedLocalConfig);
  assert.equal(fs.lstatSync(generatedSkill, { throwIfNoEntry: false }), undefined);

  const logPath = path.join(fixture.root, "source-revision.jsonl");
  let syncLogOffset = 0;
  await step("sync pending alpha-skill", () => {
    syncLogOffset = fs.statSync(logPath).size;
    return terminal.write("s");
  }, async () => {
    await locate(terminal, "Sync complete").unique().expect({ timeout: 30_000 });
    await locate(terminal, "~ in sync").unique().expect({ timeout: 30_000 });
  });
  const syncRecords = fs.readFileSync(logPath).subarray(syncLogOffset).toString("utf8")
    .split(/\r?\n/)
    .filter((line) => line.length > 0)
    .map((line) => JSON.parse(line) as { msg?: string });
  const syncEnd = syncRecords.findIndex((record) => record.msg === "pipeline sync end");
  assert.notEqual(syncEnd, -1, "sync log did not reach pipeline sync end");
  const actionRecords = syncRecords.slice(0, syncEnd + 1);
  assert.equal(
    actionRecords.filter((record) => record.msg === "built direct-Git source revision").length,
    1,
    "sync should build the locked Git source revision once before pipeline sync end",
  );
  assert.equal(fs.lstatSync(generatedSkill).isSymbolicLink(), true);
  assert.equal(fs.readFileSync(generatedContent, "utf8"), fixture.expectedAlphaSkillContent);

  await step("quit", () => terminal.write("q"), async () => {
    assert.equal((await waitForExit(terminal)).exitCode, 0);
  });

  const check = runNonTTY(fixture, ["sync", "--check"], 30_000);
  assert.equal(check.status, 0, `${check.stdout}\n${check.stderr}`);
});

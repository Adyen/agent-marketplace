import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { after, before } from "node:test";

import { defineFixture, locate, step, waitForExit } from "./helpers.js";
import { runNonTTY } from "./process-helpers.js";

// spec: tui-error-recovery/errors-preserve-specific-remediation-and-add-safe-fallbacks
const journey = defineFixture("failed-sync", ["tui"]);
before(journey.setup);
after(journey.cleanup);

journey.test("a sync that cannot write reports the error, keeps the pending change, and recovers on retry", async (terminal) => {
  const fixture = journey.current();
  const skills = path.join(fixture.repo, ".factory", "skills");
  const alphaSkill = path.join(skills, "alpha-skill");
  const repoConfig = path.join(fixture.repo, ".agents", "config.yaml");
  const screenText = async (): Promise<string> => (await terminal.text()).replace(/\s+/g, "");

  await step("settled fixture baseline", () => {}, async () => {
    await locate(terminal, "~ in sync").unique().expect({ timeout: 30_000 });
  });
  await step("select alpha-skill", () => terminal.write("G"), async () => {
    await locate(terminal, /Name:\s+alpha-skill/).unique().expect();
  });
  await step("open its toggle modal", () => terminal.write(" "), async () => {
    await locate(terminal, "Local exclude: no").unique().expect();
  });
  await step("enable it in the project", () => terminal.write("p"), async () => {
    await locate(terminal, "~ 1 out of sync").unique().expect({ timeout: 30_000 });
  });
  assert.equal(
    fs.readFileSync(repoConfig, "utf8"),
    fixture.expectedRepoConfig.replace("      - skill/beta-skill\n", "      - skill/beta-skill\n      - skill/alpha-skill\n"),
    "enabling alpha-skill did not add it next to beta-skill"
  );

  // A read-only harness folder makes the sync's write fail.
  fs.chmodSync(skills, 0o555);
  try {
    await step("the failed sync reports an error instead of success", () => terminal.write("s"), async () => {
      await locate(terminal, /! 1 errors: materialize\.wri/).unique().expect({ timeout: 30_000 });
      await locate(terminal, "~ in sync").expect({ not: true });
      await locate(terminal, /Status:\s+enabled \(pending sync\)/).unique().expect();
    });
    assert.equal(fs.lstatSync(alphaSkill, { throwIfNoEntry: false }), undefined, "the failed sync installed alpha-skill");
    await step("the errors screen keeps the cause and gives next steps", () => terminal.write("e"), async () => {
      await locate(terminal, "Errors (1)").unique().expect();
      const screen = await screenText();
      for (const expected of [
        "materialize.write.error",
        "permissiondenied",
        "Resolve the write failure at the reported path, then run `agent-marketplace sync` again".replace(/\s+/g, "")
      ]) {
        assert.ok(screen.includes(expected), `the sync error lost ${JSON.stringify(expected)}:\n${await terminal.text()}`);
      }
    });
    await step("close the errors screen", () => terminal.write("e"), async () => {
      await locate(terminal, "Errors (1)").expect({ not: true });
    });
  } finally {
    fs.chmodSync(skills, 0o755);
  }

  await step("retrying after the fix syncs and clears the error", () => terminal.write("s"), async () => {
    await locate(terminal, "~ in sync").unique().expect({ timeout: 30_000 });
    await locate(terminal, /! \d+ errors/).expect({ not: true });
  });
  assert.equal(fs.readFileSync(path.join(alphaSkill, "SKILL.md"), "utf8"), fixture.expectedAlphaSkillContent);

  await step("quit", () => terminal.write("q"), async () => {
    assert.equal((await waitForExit(terminal)).exitCode, 0);
  });
  const check = runNonTTY(fixture, ["sync", "--check"], 30_000);
  if (check.error) throw check.error;
  assert.equal(check.status, 0, `${check.stdout}\n${check.stderr}`);
});

const ownedAggregateJourney = defineFixture("owned-aggregate-recovery", ["tui"], 80, 24, { directorySource: true });
before(async () => {
  await ownedAggregateJourney.setup();
  const fixture = ownedAggregateJourney.current();
  const userConfig = path.join(fixture.home, ".agents", "config.yaml");
  const repoConfig = path.join(fixture.repo, ".agents", "config.yaml");
  const sourceHooks = path.join(fixture.registryURL, ".devin", "hooks.v1.json");
  const repoHooks = path.join(fixture.repo, ".devin", "hooks.v1.json");
  fs.writeFileSync(userConfig, "apiVersion: agent-marketplace/v4\nharnesses: [devin]\nsources: {}\n");
  fs.mkdirSync(path.dirname(sourceHooks), { recursive: true });
  fs.writeFileSync(sourceHooks, `{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"echo source"}]}]}`);
  fs.mkdirSync(path.dirname(repoConfig), { recursive: true });
  fs.writeFileSync(repoConfig, `apiVersion: agent-marketplace/v4
harnesses: [devin]
sources:
  local:
    directory: ${fixture.registryURL}
    source_harness: devin
resources:
  local:
    include: [hook/PreToolUse-command]
`);
  fs.mkdirSync(path.dirname(repoHooks), { recursive: true });
  fs.writeFileSync(repoHooks, `{"PostToolUse":[{"command":"echo mine","type":"command"}]}`);
});
after(ownedAggregateJourney.cleanup);

// spec: command-error-output/owned-aggregate-diagnostics-print-their-code-guidance
ownedAggregateJourney.test("sync recovery shows the owned Devin aggregate guidance", async (terminal) => {
  const fixture = ownedAggregateJourney.current();
  const repoConfig = path.join(fixture.repo, ".agents", "config.yaml");
  const repoHooks = path.join(fixture.repo, ".devin", "hooks.v1.json");
  const configBefore = fs.readFileSync(repoConfig);
  const flatEntry = `{"PostToolUse":[{"command":"echo mine","type":"command"}]}`;
  await step("settled launch reports the aggregate error", () => {}, async () => {
    await locate(terminal, /! 1 errors: materialize\.dev/).unique().expect({ timeout: 30_000 });
  });
  await step("the recovery copy uses catalog guidance instead of the generic fallback", () => terminal.write("e"), async () => {
    await locate(terminal, "Errors (1)").unique().expect();
    const screen = (await terminal.text()).replace(/\s+/g, "");
    const guidance = "must be JSON objects";
    assert.ok(screen.includes("materialize.devin.aggregate-shape"), `the diagnostic code is missing:\n${await terminal.text()}`);
    assert.ok(screen.includes(guidance.replace(/\s+/g, "")), `the catalog guidance is missing:\n${await terminal.text()}`);
    assert.ok(!screen.includes("agent-marketplace sync --check"), `the generic fallback is present:\n${await terminal.text()}`);
  });
  await step("close the errors screen", () => terminal.write("e"), async () => {
    await locate(terminal, "Errors (1)").expect({ not: true });
  });
  await step("retrying sync keeps the aggregate error visible", () => terminal.write("s"), async () => {
    await locate(terminal, "sync error: materialize.devin.aggregate-shape").unique().expect({ timeout: 30_000 });
  });
  assert.deepEqual(fs.readFileSync(repoConfig), configBefore, "the failed sync changed the repository configuration");
  assert.equal(fs.readFileSync(repoHooks, "utf8"), flatEntry, "the failed sync changed the handwritten aggregate");
  await step("quit after the failed sync", () => terminal.write("q"), async () => {
    assert.equal((await waitForExit(terminal)).exitCode, 0);
  });
});

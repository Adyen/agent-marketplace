import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { after, before } from "node:test";

import { defineFixture, locate, step, waitForExit } from "./helpers.js";
import { write } from "./process-helpers.js";

const journey = defineFixture("hook-review", ["tui"], 80, 24, { directorySource: true });
after(journey.cleanup);

const hook = (command: string): string => JSON.stringify({ PreToolUse: [{ matcher: "Bash", hooks: [{ type: "command", command }] }] });
before(async () => {
  await journey.setup();
  const fixture = journey.current();
  write(path.join(fixture.registryURL, "hooks", "hooks.json"), hook("echo review-v1"));
  write(path.join(fixture.home, ".agents", "config.yaml"), `apiVersion: agent-marketplace/v4\nharnesses: [factory]\nsources:\n  local:\n    directory: ${fixture.registryURL}\n    source_harness: claude\n`);
});

// spec: hook-sync-diagnostics/tui-presents-and-retains-completed-hook-review
// spec: hook-sync-diagnostics/sync-and-quit-presents-new-hook-review
journey.test("completed hook additions updates and removals stay reviewable", async (terminal) => {
  const fixture = journey.current();
  const native = path.join(fixture.repo, ".factory", "hooks.json");

  await step("settled catalog", () => {}, async () => {
    await locate(terminal, "PreToolUse-command").expect();
    await locate(terminal, "~ in sync").unique().expect();
  });
  await step("search for hook", () => terminal.write("/PreToolUse-command"), async () => {
    await locate(terminal, "1 match").expect();
  });
  await step("select hook", () => terminal.write("\r"), async () => {
    await locate(terminal, /Name:\s+PreToolUse-command/).expect();
  });
  await step("open hook scope", () => terminal.write(" "), async () => {
    await locate(terminal, "toggle project").expect();
  });
  await step("enable hook", () => terminal.write("p"), async () => {
    await locate(terminal, /Status:\s+enabled/).expect();
    await locate(terminal, "out of sync").expect();
  });
  assert.equal(fs.existsSync(native), false);
  await step("request quit with pending hook", () => terminal.write("q"), async () => {
    await locate(terminal, "Sync+quit?").expect();
  });
  await step("sync and review before quitting", () => terminal.write("y"), async () => {
    await locate(terminal, "Hook changes:").expect({ timeout: 30_000 });
    await locate(terminal, "factory (repository) add").expect();
    await locate(terminal, 'command: "echo review-v1"').expect();
    await locate(terminal, "materialize.hook.review").expect();
  });
  assert.match(fs.readFileSync(native, "utf8"), /echo review-v1/);
  await step("close completed review", () => terminal.write("\x1b"), async () => {
    await locate(terminal, "~ in sync").expect();
  });
  await step("reopen completed review", () => terminal.write("e"), async () => {
    await locate(terminal, "Hook changes:").expect();
    await locate(terminal, 'command: "echo review-v1"').expect();
  });
  await step("close review for update", () => terminal.write("\x1b"), async () => {
    await locate(terminal, "~ in sync").expect();
  });

  write(path.join(fixture.registryURL, "hooks", "hooks.json"), hook("echo review-v2"));
  await step("update hook and review replacement", () => terminal.write("u"), async () => {
    await locate(terminal, "factory (repository) replace").expect({ timeout: 30_000 });
    await locate(terminal, '- command: "echo review-v1"').expect();
    await locate(terminal, '+ command: "echo review-v2"').expect();
  });
  assert.match(fs.readFileSync(native, "utf8"), /echo review-v2/);
  await step("close replacement review", () => terminal.write("\x1b"), async () => {
    await locate(terminal, "~ in sync").expect();
  });
  await locate(terminal, "Update complete").expect({ not: true });
  await step("unchanged update clears completed review", () => terminal.write("u"), async () => {
    await locate(terminal, "Update complete").expect({ timeout: 30_000 });
    await locate(terminal, "~ in sync").expect();
    assert.doesNotMatch(await terminal.text(), /Hook changes:|materialize\.hook\.review/);
  });
  await step("open removal scope", () => terminal.write(" "), async () => {
    await locate(terminal, "toggle project").expect();
  });
  await step("deselect hook", () => terminal.write("p"), async () => {
    await locate(terminal, "out of sync").expect();
  });
  await step("sync and review removal", () => terminal.write("s"), async () => {
    await locate(terminal, "factory (repository) remove").expect({ timeout: 30_000 });
    await locate(terminal, '- command: "echo review-v2"').expect();
    await locate(terminal, "materialize.hook.review").expect();
  });
  assert.doesNotMatch(fs.readFileSync(native, "utf8"), /echo review-v[12]/);
  await step("close removal review", () => terminal.write("\x1b"), async () => {
    await locate(terminal, "~ in sync").expect();
  });
  await step("quit after review", () => terminal.write("q"), async () => {
    assert.equal((await waitForExit(terminal)).exitCode, 0);
  });
});

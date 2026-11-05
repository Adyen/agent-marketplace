import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { after, before } from "node:test";

import { defineFixture, locate, step, waitForExit } from "./helpers.js";
import { write } from "./process-helpers.js";

const journey = defineFixture("hook-review-json", ["tui"], 80, 24, { directorySource: true });
const value = "\u009b31m\u202evalue";
before(async () => {
  await journey.setup();
  const fixture = journey.current();
  write(path.join(fixture.registryURL, "hooks", "hooks.json"), JSON.stringify({ PreToolUse: [{ matcher: "Bash", hooks: [{ type: "http", url: "https://example.test/hooks", headers: { "X-Review": value } }] }] }));
  write(path.join(fixture.home, ".agents", "config.yaml"), `apiVersion: agent-marketplace/v4\nharnesses: [claude]\nsources:\n  local:\n    directory: ${fixture.registryURL}\n    source_harness: claude\n`);
});
after(journey.cleanup);

// spec: hook-sync-diagnostics/fallback-json-preserves-readable-syntax-while-escaping-controls
journey.test("HTTP hook JSON reviews escape controls without changing the installed value", async (terminal) => {
  const fixture = journey.current();
  await step("settled HTTP hook", () => {}, async () => {
    await locate(terminal, "PreToolUse-http").expect();
    await locate(terminal, "~ in sync").expect();
  });
  await step("find HTTP hook", () => terminal.write("/PreToolUse-http"), async () => {
    await locate(terminal, "1 match").expect();
  });
  await step("select HTTP hook", () => terminal.write("\r"), async () => {
    await locate(terminal, /Name:\s+PreToolUse-http/).expect();
  });
  await step("open scope", () => terminal.write(" "), async () => {
    await locate(terminal, "toggle project").expect();
  });
  await step("enable HTTP hook", () => terminal.write("p"), async () => {
    await locate(terminal, "out of sync").expect();
  });
  await step("review escaped JSON", () => terminal.write("s"), async () => {
    await locate(terminal, "Hook changes:").expect({ timeout: 30_000 });
    await locate(terminal, '"type":"http"').expect();
    await locate(terminal, "\\u009b31m\\u202evalue").expect();
  });
  const native = JSON.parse(fs.readFileSync(path.join(fixture.repo, ".claude", "settings.json"), "utf8"));
  assert.equal(native.hooks.PreToolUse[0].hooks[0].headers["X-Review"], value);
  await step("close review", () => terminal.write("\x1b"), async () => {
    await locate(terminal, "~ in sync").expect();
  });
  await step("quit", () => terminal.write("q"), async () => {
    assert.equal((await waitForExit(terminal)).exitCode, 0);
  });
});

import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { after, before } from "node:test";

import { defineFixture, locate, step, terminalOutput } from "./helpers.js";

// spec: git-authentication/ssh-prompt-cannot-reach-the-tui
const journey = defineFixture("git-transport-prompts", ["tui"], 80, 24, { adHocSource: true, sshPromptSource: true });
before(journey.setup);
after(journey.cleanup);

journey.test("SSH host-key prompts cannot reach the install-from-source TUI", async (terminal) => {
  const fixture = journey.current();
  const configPath = path.join(fixture.repo, ".agents", "config.yaml");
  const lockPath = path.join(fixture.repo, ".agents", "lock.yaml");
  const originalConfig = fs.readFileSync(configPath);
  const originalLock = fs.readFileSync(lockPath);
  const source = "ssh://git@example.invalid/owner/repo.git";
  const prompt = "Are you sure you want to continue connecting (yes/no/[fingerprint])?";

  await step("wait for the settled source list", () => {}, async () => {
    await locate(terminal, "~ in sync").unique().expect();
  });
  await step("open install-from-source", () => terminal.write("i"), async () => {
    await locate(terminal, "Install from source").unique().expect();
  });
  await step("enter the explicit SSH source", () => terminal.write(source), async () => {
    await locate(terminal, source).unique().expect();
  });
  await step("preview without a terminal prompt", () => terminal.write("\r"), async () => {
    const deadline = Date.now() + 30_000;
    let screen = "";
    while (Date.now() < deadline) {
      screen = await terminal.text();
      assert.ok(!screen.includes("Host key verification failed"), "Git host-key stderr appeared on the TUI screen");
      const flattened = screen.replace(/\s+/g, "");
      assert.ok(!flattened.includes(prompt.replace(/\s+/g, "")), "SSH host-key prompt appeared on the TUI screen");
      if (flattened.includes("git.auth-transport-failed")) break;
      await new Promise((resolve) => setTimeout(resolve, 50));
    }
    const flattened = screen.replace(/\s+/g, "");
    assert.ok(flattened.includes("git.auth-transport-failed"), `transport diagnostic was not shown:\n${screen}`);
    assert.ok(flattened.includes("example.invalid"), `diagnostic omitted the SSH host:\n${screen}`);
    assert.ok(flattened.includes("~/.ssh/known_hosts"), `diagnostic omitted known_hosts guidance:\n${screen}`);
  });

  await step("Esc closes the source modal", () => terminal.write("\x1b"), async () => {
    await locate(terminal, "Install from source").expect({ not: true });
  });
  const finalScreen = await terminal.text();
  assert.ok(!finalScreen.includes("Host key verification failed"), "Git host-key stderr appeared on the TUI screen");
  assert.deepEqual(fs.readFileSync(configPath), originalConfig);
  assert.deepEqual(fs.readFileSync(lockPath), originalLock);

  const output = await terminalOutput(terminal);
  assert.ok(!output.includes(prompt), "SSH host-key prompt was written to the terminal");
});

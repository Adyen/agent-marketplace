import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import process from "node:process";

import { expect, test } from "@microsoft/tui-test";

import {
  createFixture,
  keys,
  runNonTTY,
  targetPID,
  terminalOptions,
  waitForExit,
  waitForFile,
  writeSnapshot
} from "./helpers.js";

const general = createFixture("general");
const constrained = createFixture("constrained");
const terminated = createFixture("sigterm");
const mutation = createFixture("local-toggle-sync");

test.afterAll(() => {
  general.cleanup();
  constrained.cleanup();
  terminated.cleanup();
  mutation.cleanup();
});

test.describe("general real-TUI usage", () => {
  test.use(terminalOptions(general, [], 80, 24));

  test("launches, navigates, searches, shows help, resizes, and quits", async ({ terminal }) => {
    await expect(terminal.getByText(/Scope|Registry|Status/g, { strict: false })).toBeVisible();
    await expect(terminal.getByText("alpha-skill", { strict: false })).toBeVisible();

    terminal.write("2");
    await expect(terminal.getByText(/fixture\.git|Registry/g, { strict: false })).toBeVisible();
    terminal.write("3");
    await expect(terminal.getByText(/Available|Status/g, { strict: false })).toBeVisible();
    terminal.write("1");
    terminal.write("/");
    terminal.write("alpha-skill");
    terminal.write(keys.enter);
    await expect(terminal.getByText("alpha-skill", { strict: false })).toBeVisible();

    terminal.write("?");
    await expect(terminal.getByText("agent-marketplace — Keyboard Shortcuts", { strict: false })).toBeVisible();
    terminal.write("?");
    terminal.resize(100, 30);
    await expect(terminal.getByText(/Available\s+\(1\)|Scope\s+2 Registry\s+3 Status/g, { strict: false })).toBeVisible();
    terminal.write("g");
    await expect(terminal.getByText("Select a resource to see details", { strict: false })).toBeVisible();
    writeSnapshot(terminal, general, "general-usage");

    const exited = waitForExit(terminal);
    terminal.write("q");
    assert.equal((await exited).exitCode, 0);
  });
});

test.describe("constrained interrupt", () => {
  test.use(terminalOptions(constrained, ["tui"], 50, 12));

  test("keeps recovery controls visible and exits on Ctrl+C", async ({ terminal }) => {
    await expect(terminal.getByText("alpha-skill", { strict: false })).toBeVisible();
    await expect(terminal.getByText(/q:?\s*quit|quit\s*:?q/gi, { strict: false })).toBeVisible();
    writeSnapshot(terminal, constrained, "constrained-50x12");
    const exited = waitForExit(terminal);
    terminal.write(keys.ctrlC);
    assert.equal((await exited).exitCode, 0);
  });
});

test.describe("active SIGTERM", () => {
  test.use(terminalOptions(terminated, ["tui"], 80, 24));

  test("exits within the bounded wait", async ({ terminal }) => {
    await expect(terminal.getByText("alpha-skill", { strict: false })).toBeVisible();
    const exited = waitForExit(terminal);
    const pid = await targetPID(terminated);
    process.kill(pid, "SIGTERM");
    await exited;
    assert.throws(() => process.kill(pid, 0), (error: unknown) => (error as NodeJS.ErrnoException).code === "ESRCH");
  });
});

test.describe("local toggle and sync", () => {
  test.use(terminalOptions(mutation, ["tui"], 80, 24));

  test("writes local selection, materializes it, and reaches clean drift", async ({ terminal }) => {
    await expect(terminal.getByText("alpha-skill", { strict: false })).toBeVisible();
    terminal.write("/");
    terminal.write("alpha-skill");
    terminal.write(keys.enter);
    terminal.write("G");
    await expect(terminal.getByText(/Name:\s+alpha-skill/g, { strict: false })).toBeVisible();
    terminal.write(" ");
    await expect(terminal.getByText(/toggle local|Local:/gi, { strict: false })).toBeVisible();
    terminal.write("l");

    const localConfig = path.join(mutation.repo, ".agents", "config.local.yaml");
    await waitForFile(localConfig);
    assert.equal(fs.readFileSync(localConfig, "utf8"), mutation.expectedLocalConfig);

    terminal.write("s");
    await expect(terminal.getByText(/Sync complete|in sync/gi, { strict: false })).toBeVisible({ timeout: 30_000 });
    const generatedSkill = path.join(mutation.repo, ".factory", "skills", "alpha-skill", "SKILL.md");
    await waitForFile(generatedSkill, 30_000);

    const exited = waitForExit(terminal);
    terminal.write("q");
    assert.equal((await exited).exitCode, 0);

    const check = runNonTTY(mutation, ["sync", "--check"]);
    assert.equal(check.status, 0, `${check.stdout}\n${check.stderr}`);
    assert.equal(fs.existsSync(generatedSkill), true);
  });
});

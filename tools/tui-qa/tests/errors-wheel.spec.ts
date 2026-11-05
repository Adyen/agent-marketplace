import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { after, before } from "node:test";

import { clipboardWrites, defineFixture, locate, sgr, step, waitForExit } from "./helpers.js";
import { readRegularFile, runNonTTY } from "./process-helpers.js";

// spec: tui-mouse-navigation/scrollable-overlays-move-by-one-line
const journey = defineFixture("errors-wheel", ["tui"], 50, 12);
before(journey.setup);
after(journey.cleanup);

journey.test("the errors wheel scrolls one line without moving the list or help", async (terminal) => {
  const fixture = journey.current();
  const config = path.join(fixture.repo, ".agents", "config.yaml");
  const beta = path.join(fixture.repo, ".factory", "skills", "beta-skill");
  // Locator text joins VT soft wraps; exact row comparisons below remain the oracle.
  const frame = (screen: string) => locate(terminal, new RegExp(screen.trim().split(/\s+/)
    .map((word) => word.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")).join("\\s*")));
  const betaSelected = async (): Promise<void> => {
    await locate(terminal, /Name:\s+beta-skill/).unique().expect();
  };
  await step("settled fixture baseline", () => {}, async () => {
    await locate(terminal, "~ in sync").unique().expect({ timeout: 30_000 });
  });
  await step("select beta-skill", () => terminal.write("j"), betaSelected);
  await step("open help", () => terminal.write("?"), async () => {
    await locate(terminal, "Keyboard Shortcuts").unique().expect();
  });
  const helpTop = frame(await terminal.text());
  await step("retain a nonzero help offset", () => terminal.write("j"), async () => {
    await helpTop.expect({ not: true });
  });
  const help = await terminal.text();
  await step("close help", () => terminal.write("?"), betaSelected);

  const missing = Array.from({ length: 12 }, (_, i) => `      - skill/missing-${String(i).padStart(2, "0")}\n`).join("");
  const broken = `apiVersion: agent-marketplace/v4\nsources:\n  fixture:\n    git: ${fixture.registryURL}\n    ref: main\n    path: skills\n  required-fixture:\n    git: ${fixture.registryURL}\n    ref: main\n    path: skills\nresources:\n  fixture:\n    include:\n      - skill/beta-skill\n  required-fixture:\n    include:\n${missing}    required: true\n`;
  fs.writeFileSync(config, broken, { encoding: "utf8", mode: 0o600 });
  await step("reload produces twelve required-selection errors", () => terminal.write("r"), async () => {
    await locate(terminal, "! 12 errors").unique().expect({ timeout: 30_000 });
    await betaSelected();
  });
  const watched = [
    config, path.join(fixture.repo, ".agents", "lock.yaml"),
    path.join(fixture.home, ".agents", "config.yaml"), path.join(beta, "SKILL.md")
  ].map((file) => {
    const stat = fs.statSync(file);
    return { file, bytes: readRegularFile(file), mode: stat.mode, mtimeMs: stat.mtimeMs };
  });
  const absent = [path.join(fixture.repo, ".agents", "config.local.yaml"), path.join(fixture.home, ".agents", "lock.yaml")];
  for (const file of absent) assert.equal(fs.existsSync(file), false, `unexpected fixture file: ${file}`);
  const link = fs.readlinkSync(beta);

  await step("open overflowing errors", () => terminal.write("e"), async () => {
    await locate(terminal, "Errors (12)").unique().expect();
    await locate(terminal, "missing-00").expect();
    await locate(terminal, "missing-11").expect({ not: true });
  });
  const top = await terminal.text();
  const scroll = async (code: number, direction: "up" | "down"): Promise<void> => {
    const before = await terminal.text();
    const old = frame(before);
    await old.expect();
    await step(`wheel ${code} scrolls errors one line ${direction}`, () => terminal.write(sgr(code, 25, 5, "M")), async () => {
      await old.expect({ not: true });
      const current = (await terminal.text()).split("\n");
      const previous = before.split("\n");
      assert.deepEqual(
        direction === "down" ? current.slice(0, -2) : current.slice(1, -1),
        direction === "down" ? previous.slice(1, -1) : previous.slice(0, -2),
        "the error viewport must move by exactly one line, excluding its fixed footer"
      );
      await locate(terminal, "esc/e close").unique().expect();
      await locate(terminal, "Ctrl+C quit").unique().expect();
    });
  };
  await scroll(65, "down");
  const down = await terminal.text();
  await step("closing errors retains the underlying list selection", () => terminal.write("e"), betaSelected);
  await step("error scrolling leaves the help viewport unchanged", () => terminal.write("?"), async () => {
    await frame(help).expect();
    assert.equal(await terminal.text(), help);
  });
  await step("close unchanged help", () => terminal.write("?"), betaSelected);
  await step("reopen the scrolled errors", () => terminal.write("e"), async () => {
    await frame(down).expect();
    assert.equal(await terminal.text(), down);
  });
  await scroll(64, "up");
  assert.equal(await terminal.text(), top, "reversal must immediately restore the original viewport");
  await scroll(69, "down");
  assert.equal(await terminal.text(), down, "Shift+wheel must still scroll vertically");
  await scroll(68, "up");
  assert.equal(await terminal.text(), top);
  await step("close errors after the round trip", () => terminal.write("e"), betaSelected);
  for (const { file, bytes, mode, mtimeMs } of watched) {
    assert.deepEqual(readRegularFile(file), bytes, `scrolling changed ${file}`);
    const stat = fs.statSync(file);
    assert.equal(stat.mode, mode, `scrolling changed the mode of ${file}`);
    assert.equal(stat.mtimeMs, mtimeMs, `scrolling rewrote ${file}`);
  }
  for (const file of absent) assert.equal(fs.existsSync(file), false, `scrolling created ${file}`);
  assert.equal(fs.readlinkSync(beta), link, "scrolling re-pointed the installed skill");

  fs.writeFileSync(config, fixture.expectedRepoConfig, { encoding: "utf8", mode: 0o600 });
  await step("restoring the fixture clears errors", () => terminal.write("r"), async () => {
    await locate(terminal, "~ in sync").unique().expect({ timeout: 30_000 });
    await locate(terminal, "! 12 errors").expect({ not: true });
  });
  await step("quit", () => terminal.write("q"), async () => {
    assert.equal((await waitForExit(terminal)).exitCode, 0);
  });
  assert.deepEqual(await clipboardWrites(terminal), [], "wheel navigation must not copy text");
  const check = runNonTTY(fixture, ["sync", "--check"]);
  if (check.error) throw check.error;
  assert.equal(check.status, 0, `${check.stdout}\n${check.stderr}`);
});

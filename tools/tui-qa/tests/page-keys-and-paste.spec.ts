import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { after, before } from "node:test";

import { defineFixture, locate, step, waitForExit } from "./helpers.js";

// spec: tui-navigation/paste-in-search
// spec: tui-navigation/paste-outside-search
const journey = defineFixture("page-keys-and-paste", ["tui"], 80, 24, { overflowStatus: true });
before(journey.setup);
after(journey.cleanup);

const paste = (text: string): string => `\x1b[200~${text}\x1b[201~`;

journey.test("page keys move by half and full pages, and pasted text only ever becomes search text", async (terminal) => {
  const fixture = journey.current();
  const repoConfig = path.join(fixture.repo, ".agents", "config.yaml");
  const repoConfigBefore = fs.readFileSync(repoConfig);
  const selected = async (name: string): Promise<void> => {
    await locate(terminal, new RegExp(`Name:\\s+${name}`)).unique().expect();
  };
  const onHeader = async (): Promise<void> => {
    await locate(terminal, "Select a resource to see details").unique().expect();
  };

  await step("settled overflowing list", () => {}, async () => {
    await locate(terminal, /Enabled\s+\(31\)/).unique().expect();
    await locate(terminal, "~ in sync").unique().expect({ timeout: 30_000 });
    await onHeader();
  });
  // The list shows eleven two-line rows at 80x24: a page moves eleven rows
  // and a half page six.
  await step("ctrl+d moves half a page down", () => terminal.write("\x04"), () => selected("skill-04"));
  await step("f moves a page down", () => terminal.write("f"), () => selected("skill-15"));
  await step("page down moves another page", () => terminal.write("\x1b[6~"), () => selected("skill-26"));
  await step("ctrl+u moves half a page up", () => terminal.write("\x15"), () => selected("skill-20"));
  await step("b moves a page up", () => terminal.write("b"), () => selected("skill-09"));
  await step("page up stops at the top", () => terminal.write("\x1b[5~"), onHeader);
  await step("G jumps to the last row", () => terminal.write("G"), () => selected("alpha-skill"));
  await step("g jumps back to the top", () => terminal.write("g"), onHeader);

  await step("open search", () => terminal.write("/"), async () => {
    await locate(terminal, "enter apply · esc cancel").unique().expect();
  });
  // s and k are the sync and move-up shortcuts; pasted, they are query text.
  await step("pasted text with shortcut letters becomes the query", () => terminal.write(paste("skill-29")), async () => {
    await locate(terminal, "/ skill-29").unique().expect();
    await locate(terminal, "1 match").unique().expect();
    await locate(terminal, "~ in sync").unique().expect();
  });
  await step("apply the pasted query", () => terminal.write("\r"), () => selected("skill-29"));
  await step("clearing the search keeps skill-29 selected in the full list", () => terminal.write("\x1b"), async () => {
    await locate(terminal, "/ skill-29").expect({ not: true });
    await locate(terminal, "fixture/skill-28").unique().expect();
    await selected("skill-29");
  });

  await step("jump to the top", () => terminal.write("g"), onHeader);
  await step("select beta-skill", () => terminal.write("j"), () => selected("beta-skill"));
  await step("open its toggle modal", () => terminal.write(" "), async () => {
    await locate(terminal, "Local exclude: no").unique().expect();
  });
  // A paste of the search key, quit and the project toggle is ignored while
  // the modal is focused.
  await step("a paste into the modal changes nothing", () => terminal.write(paste("/qp")), async () => {
    await locate(terminal, "Local exclude: no").unique().expect();
    await locate(terminal, /Project ✓/).unique().expect();
    await locate(terminal, /\/ \/?qp/).expect({ not: true });
  });
  assert.deepEqual(fs.readFileSync(repoConfig), repoConfigBefore, "a paste into the toggle modal wrote the config");
  await step("close the modal", () => terminal.write("\x1b"), async () => {
    await locate(terminal, "Local exclude: no").expect({ not: true });
  });

  await step("below the minimum size the resize view replaces the list", () => terminal.resize(49, 11), async () => {
    await locate(terminal, "Resize required").unique().expect();
  });
  await step("a paste below the minimum size is ignored", () => terminal.write(paste("/skill-00")), async () => {
    await locate(terminal, "Resize required").unique().expect();
  });
  await step("growing back restores the list without a search", () => terminal.resize(80, 24), async () => {
    await locate(terminal, /Enabled\s+\(31\)/).unique().expect();
    await locate(terminal, "/ skill-00").expect({ not: true });
    await selected("beta-skill");
  });
  assert.deepEqual(fs.readFileSync(repoConfig), repoConfigBefore, "pastes outside search wrote the config");

  await step("quit", () => terminal.write("q"), async () => {
    assert.equal((await waitForExit(terminal)).exitCode, 0);
  });
});

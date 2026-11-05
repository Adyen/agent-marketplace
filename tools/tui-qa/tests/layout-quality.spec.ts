import assert from "node:assert/strict";
import { after, before } from "node:test";

import { defineFixture, locate, step, waitForExit } from "./helpers.js";

const journey = defineFixture("layout-quality", ["tui"]);
before(journey.setup);
after(journey.cleanup);

const frameTop = /╭─+╮/;
const frameBottom = /╰─+╯/;
const footerFloor = /: (commands|cmds) · \? help · q quit/;

// spec: tui-rendering/modal-at-common-sizes
journey.test("footer, palette and modals stay framed and discoverable", async (terminal) => {
  await step("footer keeps the palette, help and quit hints", () => {}, async () => {
    await locate(terminal, "beta-skill").expect();
    await locate(terminal, "~ in sync").unique().expect();
    await locate(terminal, footerFloor).unique().expect();
    await locate(terminal, "/ search").expect();
  });

  // v view is the lowest-priority footer hint: shown when the footer has
  // room, and the first hint dropped when it does not.
  await step("a wide footer shows the view-settings hint", () => terminal.resize(120, 24), async () => {
    await locate(terminal, "v view").unique().expect();
    await locate(terminal, footerFloor).unique().expect();
  });
  await step("an 80-column footer drops it first", () => terminal.resize(80, 24), async () => {
    await locate(terminal, "v view").expect({ not: true });
    await locate(terminal, footerFloor).unique().expect();
  });

  // Matched characters on unselected rows use the accent colour (ANSI 14)
  // with bold and underline, so they stay visible on terminals whose bold
  // default text is dark. The selected row renders its label as plain text.
  await step("search matches are highlighted in the accent colour", () => terminal.write("/skill\r"), async () => {
    await locate(terminal, "/ skill  2 matches").unique().expect();
    await locate(terminal, "skill").getByStyle({ foreground: "14", bold: true, underlineStyle: "single" }).expect();
  });
  await step("clear the search", () => terminal.write("\x1b"), async () => {
    await locate(terminal, "/ skill").expect({ not: true });
    // Intersect cells, so a single character left in the accent colour fails.
    await locate(terminal, "skill").and(terminal.getByStyle({ foreground: "14" })).expect({ not: true });
  });

  await step("toggle modal is one bounded frame", () => terminal.write("G "), async () => {
    await locate(terminal, "toggle local").expect();
    await locate(terminal, frameTop).unique().expect();
    await locate(terminal, frameBottom).unique().expect();
    await locate(terminal, "enter apply · esc cancel").expect();
    await locate(terminal, "┃").expect({ not: true });
  });

  await step("close the toggle modal", () => terminal.write("\x1b"), async () => {
    await locate(terminal, "toggle local").expect({ not: true });
  });

  await step("edit-config modal keeps file names", () => terminal.write("E"), async () => {
    await locate(terminal, "Edit Config").expect();
    await locate(terminal, "config.local.yaml").expect();
    await locate(terminal, frameBottom).unique().expect();
    await locate(terminal, "enter open · esc cancel").expect();
  });

  await step("close the edit-config modal", () => terminal.write("\x1b"), async () => {
    await locate(terminal, "Edit Config").expect({ not: true });
  });

  await step("minimum-size footer", () => terminal.resize(50, 12), async () => {
    await locate(terminal, footerFloor).unique().expect();
  });

  await step("palette fits the minimum size", () => terminal.write(":"), async () => {
    await locate(terminal, "Command palette").expect();
    await locate(terminal, "enter run").expect();
    await locate(terminal, footerFloor).unique().expect();
  });

  await step("close the palette", () => terminal.write("\x1b"), async () => {
    await locate(terminal, "Command palette").expect({ not: true });
  });

  await step("quit", () => terminal.write("q"), async () => {
    assert.equal((await waitForExit(terminal)).exitCode, 0);
  });
});

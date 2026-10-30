import assert from "node:assert/strict";
import { after, before } from "node:test";

import { clipboardWrites, defineFixture, findTextRun, locate, sgr, step, waitForExit } from "./helpers.js";

// spec: tui-mouse-navigation/clicking-a-section-header-toggles-its-fold
// spec: tui-mouse-navigation/clicks-that-do-not-target-a-navigable-row-are-ignored
// spec: tui-mouse-navigation/unrelated-mouse-events-are-ignored
// spec: tui-mouse-navigation/scrollable-overlays-move-by-one-line
const journey = defineFixture("mouse-headers-and-overlays", ["tui"], 80, 24, { overflowStatus: true });
before(journey.setup);
after(journey.cleanup);

const click = (x: number, y: number): string => sgr(0, x, y, "M") + sgr(0, x, y, "m");
// Wheel codes: 64/65 vertical, 66/67 horizontal; +4 adds Shift.
const wheel = (code: number, x: number, y: number): string => sgr(code, x, y, "M");

journey.test("header clicks fold, other clicks and mouse events do nothing, and the wheel scrolls help by one line", async (terminal) => {
  const betaSelected = async (): Promise<void> => {
    await locate(terminal, /Name:\s+beta-skill/).unique().expect();
    await locate(terminal, /▼ Enabled\s+\(31\)/).unique().expect();
  };
  const ignored = async (what: string, input: string): Promise<void> => {
    await step(`${what} does nothing`, async () => {
      await terminal.write(input);
      await new Promise((resolve) => setTimeout(resolve, 250));
    }, betaSelected);
  };

  await step("settled overflowing catalog", () => {}, async () => {
    await locate(terminal, /Enabled\s+\(31\)/).unique().expect();
    await locate(terminal, "~ in sync").unique().expect({ timeout: 30_000 });
  });
  await step("select beta-skill", () => terminal.write("j"), betaSelected);

  const header = await findTextRun(terminal, /Enabled\s+\(31\)/);
  await step("clicking the Enabled header selects and folds it", () => terminal.write(click(header.x, header.y)), async () => {
    await locate(terminal, /▶ Enabled\s+\(31\)/).unique().expect();
    await locate(terminal, "fixture/beta-skill").expect({ not: true });
    await locate(terminal, "Select a resource to see details").unique().expect();
  });
  await step("clicking it again unfolds it", () => terminal.write(click(header.x, header.y)), async () => {
    await locate(terminal, /▼ Enabled\s+\(31\)/).unique().expect();
    await locate(terminal, "fixture/beta-skill").unique().expect();
  });
  await step("select beta-skill again", () => terminal.write("j"), betaSelected);

  const beta = await findTextRun(terminal, /fixture\/beta-skill/);
  await ignored("a click on the title row", click(5, 0));
  await ignored("a click in the right-hand pane", click(60, 10));
  await ignored("a click on the footer", click(5, 23));
  await ignored("a release without a press", sgr(0, beta.x, beta.y, "m"));
  await ignored("a horizontal wheel over the list", wheel(67, beta.x, beta.y));
  await step("a Shift-modified vertical wheel still scrolls the list", () => terminal.write(wheel(69, beta.x, beta.y)), async () => {
    await locate(terminal, /Name:\s+skill-00/).unique().expect();
  });

  await step("open help", () => terminal.write("?"), async () => {
    await locate(terminal, "Keyboard Shortcuts").unique().expect();
    await locate(terminal, "zM            collapse all").expect({ not: true });
  });
  await step("a wheel down scrolls help by one line", () => terminal.write(wheel(65, 40, 10)), async () => {
    await locate(terminal, "zM            collapse all").unique().expect();
    await locate(terminal, "zR            expand all").expect({ not: true });
  });
  await step("another wheel down scrolls one more line", () => terminal.write(wheel(65, 40, 10)), async () => {
    await locate(terminal, "zR            expand all").unique().expect();
  });
  await step("a wheel up scrolls back one line", () => terminal.write(wheel(64, 40, 10)), async () => {
    await locate(terminal, "zR            expand all").expect({ not: true });
    await locate(terminal, "zM            collapse all").unique().expect();
  });
  await step("a click inside help does nothing", async () => {
    await terminal.write(click(10, 8));
    await new Promise((resolve) => setTimeout(resolve, 250));
  }, async () => {
    await locate(terminal, "Keyboard Shortcuts").unique().expect();
    await locate(terminal, "zM            collapse all").unique().expect();
  });
  await step("esc closes help on the same selection", () => terminal.write("\x1b"), async () => {
    await locate(terminal, "Keyboard Shortcuts").expect({ not: true });
    await locate(terminal, /Name:\s+skill-00/).unique().expect();
  });

  await step("quit", () => terminal.write("q"), async () => {
    assert.equal((await waitForExit(terminal)).exitCode, 0);
  });
  assert.deepEqual(await clipboardWrites(terminal), []);
});

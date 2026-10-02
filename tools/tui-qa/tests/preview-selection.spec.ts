import assert from "node:assert/strict";

import { expect, test } from "@microsoft/tui-test";

import { declineLegacyMigration, defineFixture, step, waitForExit, type TestTerminal } from "./helpers.js";

const journey = defineFixture("preview-selection", ["tui"], 80, 24, { overflowStatus: true });
test.use(journey.options);
test.beforeAll(journey.setup);
test.afterAll(journey.cleanup);

const sgr = (code: number, x: number, y: number, final: "M" | "m"): string =>
  `\x1b[<${code};${x + 1};${y + 1}${final}`;
const drag = (x0: number, y0: number, x1: number, y1: number): string =>
  sgr(0, x0, y0, "M") + sgr(32, x1, y1, "M") + sgr(0, x1, y1, "m");

const osc52Capture = (terminal: TestTerminal): { payloads(): string[] } => {
  const pty = (terminal as unknown as { _pty?: { onData(cb: (data: string) => void): void } })._pty;
  assert.ok(pty, "test terminal does not expose a PTY output stream");
  const chunks: string[] = [];
  pty.onData((data) => chunks.push(data));
  return {
    payloads(): string[] {
      return [...chunks.join("").matchAll(/\x1b\]52;[^;\x07\x1b]*;([^\x07\x1b]*)[\x07\x1b]/g)].map(
        (match) => match[1]
      );
    },
  };
};

interface HeadlessCell {
  isInverse(): number;
}
interface HeadlessLine {
  translateToString(trimRight?: boolean): string;
  getCell(x: number): HeadlessCell | undefined;
  readonly length: number;
}
interface HeadlessBuffer {
  baseY: number;
  length: number;
  getLine(y: number): HeadlessLine | undefined;
}

const xterm = (terminal: TestTerminal): { buffer: { active: HeadlessBuffer } } =>
  (terminal as unknown as { _term: { buffer: { active: HeadlessBuffer } } })._term;

interface TextRun {
  x: number;
  y: number;
  text: string;
  cells: HeadlessCell[];
}

const findTextRun = (terminal: TestTerminal, text: RegExp): TextRun | undefined => {
  const buffer = xterm(terminal).buffer.active;
  for (let y = buffer.baseY; y < buffer.length; y++) {
    const line = buffer.getLine(y);
    if (!line) continue;
    const match = text.exec(line.translateToString(false));
    if (!match) continue;
    const cells: HeadlessCell[] = [];
    for (let x = match.index; x < match.index + match[0].length; x++) {
      const cell = line.getCell(x);
      if (cell) cells.push(cell);
    }
    return { x: match.index, y: y - buffer.baseY, text: match[0], cells };
  }
  return undefined;
};

const waitForInverse = async (
  terminal: TestTerminal,
  text: RegExp,
  want: number
): Promise<void> => {
  const deadline = Date.now() + 5_000;
  const wanted = want !== 0;
  let run: TextRun | undefined;
  while (Date.now() < deadline) {
    run = findTextRun(terminal, text);
    if (run && run.cells.length > 0 && run.cells.every((cell) => (cell.isInverse() !== 0) === wanted)) return;
    await new Promise((resolve) => setTimeout(resolve, 50));
  }
  assert.ok(run, `expected visible text matching ${text}`);
  assert.ok(
    run.cells.every((cell) => (cell.isInverse() !== 0) === wanted),
    `expected ${JSON.stringify(run.text)} at (${run.x},${run.y}) to be inverse=${want}`
  );
};

// spec: tui-mouse-navigation/terminal-gestures-deliver-only-the-selected-text
test("right-pane drag highlights, copies on release, and leaves wheel and clicks untouched", async ({ terminal }) => {
  const osc52 = osc52Capture(terminal);
  await declineLegacyMigration(terminal);

  await step("settled overflowing catalog", () => {}, async () => {
    await expect(terminal.getByText("beta-skill", { strict: false })).toBeVisible();
  });

  await step("wheel scroll still routes and emits no clipboard write", () => terminal.write("\x1b[<65;4;6M\x1b[<65;4;6M"), async () => {
    await expect(terminal.getByText(/Name:\s+skill-00/g)).toBeVisible();
    assert.equal(osc52.payloads().length, 0);
  });

  let dragSent = "";
  await step("drag across the detail pane highlights and copies on release", async () => {
    const run = findTextRun(terminal, /Name:\s+skill-00/g);
    assert.ok(run, "detail pane does not show the focused resource name");
    dragSent = drag(run.x, run.y, run.x + run.text.length - 1, run.y);
    await terminal.write(dragSent);
  }, async () => {
    try {
      await waitForInverse(terminal, /Name:\s+skill-00/g, 1);
    } catch (error) {
      const { shifts } = terminal.serialize();
      throw new Error(
        `${(error as Error).message}; drag=${JSON.stringify(dragSent)}; payloads=${JSON.stringify(osc52.payloads())}; ` +
          `shifts=${JSON.stringify([...shifts.entries()].filter(([, v]) => v.inverse !== undefined))}`
      );
    }
    const payloads = osc52.payloads();
    assert.equal(payloads.length, 1);
    assert.equal(Buffer.from(payloads[0], "base64").toString(), "Name:     skill-00");
  });

  await step("wheel scroll clears the active highlight without a clipboard write", () => terminal.write("\x1b[<65;4;6M"), async () => {
    await expect(terminal.getByText(/Name:\s+skill-01/g)).toBeVisible();
    await waitForInverse(terminal, /Name:\s+skill-01/g, 0);
    assert.equal(osc52.payloads().length, 1);
  });

  await step("a click without drag copies nothing", () => terminal.write(drag(70, 5, 70, 5)), async () => {
    await waitForInverse(terminal, /Name:\s+skill-01/g, 0);
    assert.equal(osc52.payloads().length, 1);
  });

  await step("a list-side drag selects nothing", () => terminal.write(drag(2, 3, 20, 3)), async () => {
    await waitForInverse(terminal, /Name:\s+skill-01/g, 0);
    assert.equal(osc52.payloads().length, 1);
  });

  await step("quit without further input", () => terminal.write("q"), async () => {
    assert.equal((await waitForExit(terminal)).exitCode, 0);
  });
});

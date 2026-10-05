import assert from "node:assert/strict";

import { expect, test } from "@microsoft/tui-test";

import { createFixture, type Fixture, type FixtureOptions } from "./fixture.js";

type TestBody = Parameters<typeof test>[1];
type TestOptions = Parameters<typeof test.use>[0];
type StepPart = () => void | Promise<void>;
export type TestTerminal = Parameters<TestBody>[0]["terminal"];

interface FixtureLifecycle {
  options: TestOptions;
  setup(): void | Promise<void>;
  cleanup(): void;
  current(): Fixture;
}

export const defineFixture = (
  name: string,
  args: string[],
  columns: 50 | 80 = 80,
  rows: 12 | 24 = 24,
  options: FixtureOptions = {}
): FixtureLifecycle => {
  const env: Record<string, string> = {};
  const program = { file: "true", args: [] as string[] };
  let fixture: Fixture | undefined;
  return {
    options: { columns, rows, env, program },
    setup: async () => {
      fixture = await createFixture(name, options);
      Object.assign(env, fixture.env);
      program.file = fixture.launcher;
      program.args = [...args];
    },
    cleanup: () => {
      fixture?.cleanup();
      fixture = undefined;
    },
    current: () => {
      if (!fixture) throw new Error(`fixture ${name} is not set up`);
      return fixture;
    }
  };
};

export const declineLegacyMigration = async (terminal: TestTerminal): Promise<void> => {
  await expect(terminal.getByText("Configuration migration required", { strict: false })).toBeVisible();
  await terminal.write("l");
  await expect(terminal.getByText("Config left unchanged")).toBeVisible();
};

export const step = async (name: string, action: StepPart, assertion: StepPart): Promise<void> => {
  await action();
  try {
    await assertion();
  } catch (error) {
    if (error instanceof Error) {
      error.message = `${name}: ${error.message}`;
      if (error.stack) {
        const stack = error.stack.split("\n");
        stack[0] = `${error.name}: ${error.message}`;
        error.stack = stack.join("\n");
      }
    }
    throw error;
  }
};

export const sgr = (code: number, x: number, y: number, final: "M" | "m"): string =>
  `\x1b[<${code};${x + 1};${y + 1}${final}`;
export const sgrDrag = (x0: number, y0: number, x1: number, y1: number): string =>
  sgr(0, x0, y0, "M") + sgr(32, x1, y1, "M") + sgr(0, x1, y1, "m");

export const osc52Capture = (terminal: TestTerminal): { payloads(): string[] } => {
  const pty = (terminal as unknown as { _pty?: { onData(cb: (data: string) => void): void } })._pty;
  assert.ok(pty, "test terminal does not expose a PTY output stream");
  const chunks: string[] = [];
  pty.onData((data) => chunks.push(data));
  return {
    payloads(): string[] {
      return [...chunks.join("").matchAll(/\x1b\]52;[^;\x07\x1b]*;([^\x07\x1b]*)[\x07\x1b]/g)].map(
        (match) => match[1]
      );
    }
  };
};

interface HeadlessCell {
  isInverse(): number;
  getChars(): string;
  getWidth(): number;
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

export interface TextRun {
  x: number;
  y: number;
  text: string;
  cells: HeadlessCell[];
}

export const findTextRun = (terminal: TestTerminal, text: RegExp): TextRun | undefined => {
  const buffer = xterm(terminal).buffer.active;
  for (let y = buffer.baseY; y < buffer.length; y++) {
    const line = buffer.getLine(y);
    if (!line) continue;
    let composed = "";
    const cellStart: number[] = [];
    for (let x = 0; x < line.length; x++) {
      cellStart.push(composed.length);
      const cell = line.getCell(x);
      if (cell && cell.getWidth() === 0) continue;
      composed += cell?.getChars() || " ";
    }
    const match = text.exec(composed);
    if (!match) continue;
    const firstCell = cellStart.findIndex((offset) => offset > match.index) - 1;
    const x0 = firstCell < 0 ? cellStart.length - 1 : firstCell;
    const endOffset = match.index + match[0].length - 1;
    let x1 = cellStart.findIndex((offset) => offset > endOffset) - 1;
    if (x1 < x0) x1 = x0;
    for (let x = x1 + 1; x < line.length; x++) {
      const next = line.getCell(x);
      if (!next || next.getWidth() !== 0) break;
      x1 = x;
    }
    const cells: HeadlessCell[] = [];
    for (let x = x0; x <= x1; x++) {
      const cell = line.getCell(x);
      if (cell) cells.push(cell);
    }
    return { x: x0, y: y - buffer.baseY, text: match[0], cells };
  }
  return undefined;
};

export const waitForInverse = async (
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

export const waitForExit = (terminal: TestTerminal, timeoutMs = 5_000): Promise<{ exitCode: number; signal?: number }> => {
  if (terminal.exitResult) return Promise.resolve(terminal.exitResult);
  return new Promise((resolve, reject) => {
    const timeout = setTimeout(() => reject(new Error("TUI process did not exit within the bounded wait")), timeoutMs);
    terminal.onExit((result) => {
      clearTimeout(timeout);
      resolve(result);
    });
  });
};

import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { test } from "node:test";

import { TuiTest, uniqueSession, type Locator } from "@microsoft/tui-test";

import { createFixture, type Fixture, type FixtureOptions } from "./fixture.js";
import { artifactRoot } from "./process-helpers.js";

type StepPart = () => void | Promise<void>;
export type TestTerminal = TuiTest;
type JourneyBody = (terminal: TestTerminal) => Promise<void>;

interface FixtureLifecycle {
  setup(): Promise<void>;
  cleanup(): void;
  current(): Fixture;
  test(title: string, body: JourneyBody): void;
}

const journeyDirectories = new WeakMap<TestTerminal, string>();
const closed = new WeakSet<TestTerminal>();

const amend = (error: unknown, message: (original: string) => string): void => {
  if (!(error instanceof Error) || !error.message) return;
  const original = error.message;
  error.message = message(original);
  if (error.stack?.includes(original)) error.stack = error.stack.replace(original, () => error.message);
};

// finish renders the GIF only when it may be kept: stopRecording encodes the
// whole capture, while closing an unstopped recording discards it.
const finish = async (terminal: TestTerminal, failed: boolean, render = failed): Promise<void> => {
  if (closed.has(terminal)) return;
  closed.add(terminal);
  if (render) await terminal.stopRecording().catch(() => undefined);
  await terminal.close({ failed }).catch((error: unknown) => {
    if (!failed) throw error;
  });
};

// Each journey runs in its own tui-test session, records a GIF, and writes a
// trace on close. A failing journey keeps the GIF, the trace (trace.html,
// trace.md and session.cast) under <artifactRoot>/<journey>/; a passing
// journey removes them without rendering the GIF.
const runJourney = async (
  journey: string,
  fixture: Fixture,
  args: string[],
  cols: number,
  rows: number,
  body: JourneyBody
): Promise<void> => {
  const directory = path.join(artifactRoot, journey);
  fs.rmSync(directory, { recursive: true, force: true });
  fs.mkdirSync(directory, { recursive: true });
  const terminal = new TuiTest(uniqueSession(journey), {
    recording: { directory: path.join(directory, "casts") },
    trace: { mode: "on", directory }
  });
  journeyDirectories.set(terminal, directory);
  let failed = true;
  try {
    // tui-test adds its own environment to the child's; env -i keeps the
    // program to exactly the fixture's sandboxed environment.
    const environment = Object.entries(fixture.env).map(([key, value]) => `${key}=${value}`);
    await terminal.run("/usr/bin/env", ["-i", ...environment, fixture.launcher, ...args], { cols, rows });
    await terminal.startRecording(path.join(directory, `${journey}.gif`));
    await body(terminal);
    failed = false;
  } catch (error) {
    const screen = closed.has(terminal) ? "(session closed)" : await terminal.text().catch(() => "(unavailable)");
    amend(error, (message) => `${message}\n\nTerminal screen:\n${screen}\n\nRecording and trace: ${directory}`);
    throw error;
  } finally {
    await finish(terminal, failed);
    if (!failed) fs.rmSync(directory, { recursive: true, force: true });
  }
};

export const defineFixture = (
  name: string,
  args: string[],
  columns: 50 | 80 = 80,
  rows: 12 | 24 = 24,
  options: FixtureOptions = {}
): FixtureLifecycle => {
  let fixture: Fixture | undefined;
  let journeys = 0;
  const current = (): Fixture => {
    if (!fixture) throw new Error(`fixture ${name} is not set up`);
    return fixture;
  };
  return {
    setup: async () => {
      fixture = await createFixture(name, options);
    },
    cleanup: () => {
      fixture?.cleanup();
      fixture = undefined;
    },
    current,
    test: (title, body) => {
      journeys += 1;
      const journey = journeys === 1 ? name : `${name}-${journeys}`;
      test(title, () => runJourney(journey, current(), args, columns, rows, body));
    }
  };
};

export const step = async (name: string, action: StepPart, assertion: StepPart): Promise<void> => {
  await action();
  try {
    await assertion();
  } catch (error) {
    amend(error, (message) => `${name}: ${message}`);
    throw error;
  }
};

// locate matches literal text, or a flagless RegExp's source in tui-test's
// regex syntax, on the visible screen.
export const locate = (terminal: TestTerminal, pattern: string | RegExp): Locator => {
  if (typeof pattern === "string") return terminal.getByText(pattern);
  assert.equal(pattern.flags, "", `locator ${pattern} must not carry flags`);
  return terminal.getByText(pattern.source, { regex: true });
};

export const sgr = (code: number, x: number, y: number, final: "M" | "m"): string =>
  `\x1b[<${code};${x + 1};${y + 1}${final}`;
export const sgrDrag = (x0: number, y0: number, x1: number, y1: number): string =>
  sgr(0, x0, y0, "M") + sgr(32, x1, y1, "M") + sgr(0, x1, y1, "m");

// clipboardWrites ends the session and returns every OSC 52 payload the
// program wrote, decoded, from the trace's session cast. tui-test writes the
// trace only on close, so call it last, once the journey has quit the TUI.
export const clipboardWrites = async (terminal: TestTerminal): Promise<string[]> => {
  await finish(terminal, false, true);
  const directory = journeyDirectories.get(terminal);
  assert.ok(directory, "terminal was not started by a journey");
  const casts = fs
    .readdirSync(directory, { withFileTypes: true })
    .filter((entry) => entry.isDirectory() && fs.existsSync(path.join(directory, entry.name, "session.cast")));
  assert.equal(casts.length, 1, `expected one trace with a session cast in ${directory}`);
  const output = fs
    .readFileSync(path.join(directory, casts[0].name, "session.cast"), "utf8")
    .split("\n")
    .slice(1)
    .filter(Boolean)
    .map((line) => JSON.parse(line) as [number, string, string])
    .filter(([, kind]) => kind === "o")
    .map(([, , data]) => data)
    .join("");
  return [...output.matchAll(/\x1b\]52;[^;\x07\x1b]*;([^\x07\x1b]*)[\x07\x1b]/g)].map((match) =>
    Buffer.from(match[1], "base64").toString()
  );
};

export interface TextRun {
  x: number;
  y: number;
  width: number;
  text: string;
}

export const findTextRun = async (terminal: TestTerminal, text: RegExp): Promise<TextRun> => {
  const locator = locate(terminal, text).first();
  await locator.expect();
  const match = await locator.location();
  assert.equal(match.start.row, match.end.row, `expected ${text} on one row`);
  return { x: match.start.column, y: match.start.row, width: match.end.column - match.start.column, text: match.text };
};

export const waitForInverse = (terminal: TestTerminal, text: RegExp, want: number): Promise<void> =>
  locate(terminal, text).getByStyle({ inverse: want !== 0 }).expect();

export const waitForExit = async (
  terminal: TestTerminal,
  timeoutMs = 5_000
): Promise<{ exitCode: number | null; signal: string | null }> => {
  await terminal.waitExit({ timeout: timeoutMs });
  const state = await terminal.state();
  return { exitCode: state.exited, signal: state.exit_signal };
};

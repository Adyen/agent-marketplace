import { expect, test } from "@microsoft/tui-test";

import { createFixture, type Fixture, type FixtureOptions } from "./fixture.js";

export type { Fixture, FixtureOptions };
export type OrganizationFixtureMode = import("./fixture.js").OrganizationFixtureMode;

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

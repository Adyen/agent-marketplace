import { execFileSync } from "node:child_process";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { pathToFileURL } from "node:url";

import { expect, test } from "@microsoft/tui-test";

import { binary, isolatedEnvironment, write } from "./process-helpers.js";

type TestBody = Parameters<typeof test>[1];
type TestOptions = Parameters<typeof test.use>[0];
type StepPart = () => void | Promise<void>;
export type TestTerminal = Parameters<TestBody>[0]["terminal"];

const alphaSkillContent = "---\nname: alpha-skill\ndescription: Available alpha fixture skill\n---\n\n# Alpha Skill\n\nAlpha fixture body.\n";
const betaSkillContent = "---\nname: beta-skill\ndescription: Synced beta fixture skill\n---\n\n# Beta Skill\n\nBeta fixture body.\n";

export interface Fixture {
  root: string;
  home: string;
  repo: string;
  launcher: string;
  pidFile: string;
  registryURL: string;
  env: Record<string, string>;
  expectedAlphaSkillContent: string;
  expectedLocalConfig: string;
  cleanup(): void;
}

interface FixtureLifecycle {
  options: TestOptions;
  setup(): void;
  cleanup(): void;
  current(): Fixture;
}

const git = (cwd: string, env: Record<string, string>, args: string[]): void => {
  execFileSync("git", args, { cwd, env, stdio: ["ignore", "pipe", "pipe"], timeout: 30_000 });
};

const shellQuote = (value: string): string => `'${value.replaceAll("'", `'"'"'`)}'`;

const createFixture = (name: string): Fixture => {
  if (!/^[a-z0-9-]+$/.test(name)) throw new Error(`unsafe fixture name ${JSON.stringify(name)}`);
  if (!fs.existsSync(binary)) throw new Error(`TUI QA binary not found: ${binary}`);
  const root = fs.mkdtempSync(path.join(os.tmpdir(), `agent-marketplace-tui-${name}-`));
  try {
    const home = path.join(root, "home");
    const repo = path.join(root, "workspace");
    const registries = path.join(root, "registries");
    const tmp = path.join(root, "tmp");
    const xdg = path.join(root, "xdg");
    for (const directory of [home, repo, registries, tmp, xdg]) fs.mkdirSync(directory, { recursive: true, mode: 0o700 });

    const env = isolatedEnvironment(root, home, tmp, xdg);
    const source = path.join(registries, "source");
    write(path.join(source, "skills", "alpha-skill", "SKILL.md"), alphaSkillContent);
    write(path.join(source, "skills", "beta-skill", "SKILL.md"), betaSkillContent);
    git(source, env, ["init", "--quiet", "--initial-branch=main"]);
    git(source, env, ["add", "."]);
    git(source, env, ["-c", "user.name=TUI QA", "-c", "user.email=tui-qa@example.invalid", "commit", "--quiet", "-m", "Seed TUI fixture"]);
    const bareRegistry = path.join(registries, "fixture.git");
    git(registries, env, ["clone", "--quiet", "--bare", source, bareRegistry]);
    const registryURL = pathToFileURL(bareRegistry).href;

    write(
      path.join(home, ".agents", "config.yaml"),
      "apiVersion: agent-marketplace/v2\nharnesses:\n  - factory\nregistries: []\n"
    );
    write(
      path.join(repo, ".agents", "config.yaml"),
      `apiVersion: agent-marketplace/v2\nregistries:\n  - url: ${registryURL}\n    ref: main\n    path: skills\n    skills:\n      include:\n        - beta-skill\n`
    );
    git(repo, env, ["init", "--quiet", "--initial-branch=main"]);
    execFileSync(binary, ["sync"], { cwd: repo, env, stdio: ["ignore", "pipe", "pipe"], timeout: 30_000 });

    const betaSkill = path.join(repo, ".factory", "skills", "beta-skill");
    if (!fs.lstatSync(betaSkill).isSymbolicLink()) throw new Error("fixture beta-skill is not a symlink");
    if (fs.lstatSync(path.join(repo, ".factory", "skills", "alpha-skill"), { throwIfNoEntry: false })) throw new Error("fixture alpha-skill was unexpectedly materialized");

    const launcher = path.join(root, "launch-agent-marketplace");
    const pidFile = path.join(root, "target.pid");
    write(launcher, `#!/bin/sh\ncd -- ${shellQuote(repo)}\nprintf '%s\\n' "$$" > ${shellQuote(pidFile)}\nexec ${shellQuote(binary)} "$@"\n`, 0o700);

    return {
      root,
      home,
      repo,
      launcher,
      pidFile,
      registryURL,
      env,
      expectedAlphaSkillContent: alphaSkillContent,
      expectedLocalConfig: `registries:\n  - url: ${registryURL}\n    ref: main\n    path: skills\n    skills:\n      include:\n        - alpha-skill\n`,
      cleanup: () => fs.rmSync(root, { recursive: true, force: true })
    };
  } catch (error) {
    fs.rmSync(root, { recursive: true, force: true });
    throw error;
  }
};

export const defineFixture = (
  name: string,
  args: string[],
  columns: 50 | 80 = 80,
  rows: 12 | 24 = 24
): FixtureLifecycle => {
  const env: Record<string, string> = {};
  const program = { file: "true", args: [] as string[] };
  let fixture: Fixture | undefined;
  return {
    options: { columns, rows, env, program },
    setup: () => {
      fixture = createFixture(name);
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

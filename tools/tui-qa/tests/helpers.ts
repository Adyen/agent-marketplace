import { execFileSync, spawnSync } from "node:child_process";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import process from "node:process";
import { fileURLToPath, pathToFileURL } from "node:url";

import { test } from "@microsoft/tui-test";

type TestBody = Parameters<typeof test>[1];
type TestOptions = Parameters<typeof test.use>[0];
export type TestTerminal = Parameters<TestBody>[0]["terminal"];

const moduleDirectory = path.dirname(fileURLToPath(import.meta.url));
const packageRoot = [
  process.cwd(),
  path.resolve(moduleDirectory, ".."),
  path.resolve(moduleDirectory, "..", ".."),
  path.resolve(moduleDirectory, "..", "..", "..")
].find((candidate) => fs.existsSync(path.join(candidate, "package.json")));
if (!packageRoot) throw new Error("cannot locate the TUI QA package root");
export const repositoryRoot = path.resolve(process.env.AM_TUI_QA_REPO_ROOT ?? path.join(packageRoot, "..", ".."));
export const binary = path.resolve(process.env.AM_TUI_QA_BINARY ?? path.join(repositoryRoot, ".local", "agent-marketplace", "bin", "agent-marketplace-tui-qa"));
const runID = process.env.AM_TUI_QA_RUN_ID ?? `tui-qa-${Date.now()}`;
if (!/^[A-Za-z0-9._-]+$/.test(runID)) throw new Error("AM_TUI_QA_RUN_ID contains unsafe path characters");
export const artifactRoot = path.resolve(
  process.env.AM_TUI_QA_ARTIFACT_DIR ?? path.join(repositoryRoot, ".local", "agent-marketplace", "qa", "tui", runID)
);

export interface Fixture {
  root: string;
  home: string;
  repo: string;
  launcher: string;
  pidFile: string;
  registryURL: string;
  env: Record<string, string>;
  expectedLocalConfig: string;
  cleanup(): void;
}

const git = (cwd: string, env: Record<string, string>, args: string[]): void => {
  execFileSync("git", args, { cwd, env, stdio: ["ignore", "pipe", "pipe"], timeout: 30_000 });
};

const write = (file: string, content: string, mode = 0o600): void => {
  fs.mkdirSync(path.dirname(file), { recursive: true, mode: 0o700 });
  fs.writeFileSync(file, content, { encoding: "utf8", mode });
};

const shellQuote = (value: string): string => `'${value.replaceAll("'", `'"'"'`)}'`;

export const createFixture = (name: string): Fixture => {
  if (!/^[a-z0-9-]+$/.test(name)) throw new Error(`unsafe fixture name ${JSON.stringify(name)}`);
  if (!fs.existsSync(binary)) throw new Error(`TUI QA binary not found: ${binary}`);
  const root = fs.mkdtempSync(path.join(os.tmpdir(), `agent-marketplace-tui-${name}-`));
  const home = path.join(root, "home");
  const repo = path.join(root, "workspace");
  const registries = path.join(root, "registries");
  const tmp = path.join(root, "tmp");
  const xdg = path.join(root, "xdg");
  for (const directory of [home, repo, registries, tmp, xdg]) fs.mkdirSync(directory, { recursive: true, mode: 0o700 });

  const env: Record<string, string> = {
    PATH: "/usr/local/bin:/usr/bin:/bin",
    HOME: home,
    USER: "tui-qa",
    LOGNAME: "tui-qa",
    XDG_CONFIG_HOME: path.join(xdg, "config"),
    XDG_CACHE_HOME: path.join(xdg, "cache"),
    XDG_DATA_HOME: path.join(xdg, "data"),
    XDG_STATE_HOME: path.join(xdg, "state"),
    TMPDIR: tmp,
    TMP: tmp,
    TEMP: tmp,
    CODEX_HOME: path.join(home, ".codex"),
    GIT_CONFIG_NOSYSTEM: "1",
    GIT_CONFIG_SYSTEM: "/dev/null",
    GIT_CONFIG_GLOBAL: "/dev/null",
    GIT_CONFIG_COUNT: "3",
    GIT_CONFIG_KEY_0: "core.hooksPath",
    GIT_CONFIG_VALUE_0: "/dev/null",
    GIT_CONFIG_KEY_1: "credential.helper",
    GIT_CONFIG_VALUE_1: "",
    GIT_CONFIG_KEY_2: "protocol.file.allow",
    GIT_CONFIG_VALUE_2: "always",
    GIT_ASKPASS: "/bin/false",
    GIT_TERMINAL_PROMPT: "0",
    GIT_SSH_COMMAND: "/bin/false",
    SSH_ASKPASS: "/bin/false",
    SSH_ASKPASS_REQUIRE: "never",
    AGENT_MARKETPLACE_LOG_ENABLED: "false",
    AGENT_MARKETPLACE_V3_ORG_CONFIG: path.join(root, "missing-organization-config.yaml"),
    TERM: "xterm-256color",
    LANG: "C.UTF-8"
  };

  const source = path.join(registries, "source");
  write(
    path.join(source, "skills", "alpha-skill", "SKILL.md"),
    "---\nname: alpha-skill\ndescription: Synthetic real-TUI fixture\n---\n\n# Alpha Skill\n"
  );
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
    `apiVersion: agent-marketplace/v2\nregistries:\n  - url: ${registryURL}\n    ref: main\n    path: skills\n`
  );
  git(repo, env, ["init", "--quiet", "--initial-branch=main"]);
  execFileSync(binary, ["sync"], { cwd: repo, env, stdio: ["ignore", "pipe", "pipe"], timeout: 30_000 });
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
    expectedLocalConfig: `registries:\n  - url: ${registryURL}\n    ref: main\n    path: skills\n    skills:\n      include:\n        - alpha-skill\n`,
    cleanup: () => fs.rmSync(root, { recursive: true, force: true })
  };
};

export const terminalOptions = (
  fixture: Fixture,
  args: string[],
  columns: 50 | 80 = 80,
  rows: 12 | 24 = 24
): TestOptions => ({ columns, rows, env: fixture.env, program: { file: fixture.launcher, args } });

export const keys = {
  enter: "\r",
  esc: "\u001b",
  ctrlC: "\u0003"
};

export const waitForExit = async (terminal: TestTerminal, timeoutMs = 5_000): Promise<{ exitCode: number; signal?: number }> => {
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    if (terminal.exitResult) return terminal.exitResult;
    await new Promise((resolve) => setTimeout(resolve, 10));
  }
  throw new Error("TUI process did not exit within the bounded wait");
};

export const targetPID = async (fixture: Fixture, timeoutMs = 5_000): Promise<number> => {
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    try {
      if (fs.existsSync(fixture.pidFile)) {
        const pid = Number(fs.readFileSync(fixture.pidFile, "utf8").trim());
        if (Number.isSafeInteger(pid) && pid > 1) return pid;
      }
    } catch (error) {
      if ((error as NodeJS.ErrnoException).code !== "ENOENT") throw error;
    }
    await new Promise((resolve) => setTimeout(resolve, 10));
  }
  throw new Error("real TUI process did not publish its PID within the bounded wait");
};

export const privateValues = (): string[] => {
  const values = (process.env.AM_TUI_QA_PRIVATE_VALUES ?? "")
    .split("\n")
    .map((value) => value.trim())
    .filter(Boolean);
  if (values.some((value) => value.length < 3)) throw new Error("AM_TUI_QA_PRIVATE_VALUES entries must contain at least three characters");
  return values;
};

const sanitizeSnapshot = (value: string, fixture: Fixture): string => {
  let sanitized = value
    .replaceAll(fixture.root, "<fixture>")
    .replaceAll(repositoryRoot, "<repo>")
    .replace(/[\u0000-\u0008\u000b\u000c\u000e-\u001f\u007f-\u009f]/g, (character) => `\\x${character.charCodeAt(0).toString(16).padStart(2, "0")}`)
    .replace(/[\u061c\u200e\u200f\u202a-\u202e\u2066-\u2069]/g, "<bidi>");
  for (const privateValue of privateValues()) sanitized = sanitized.replaceAll(privateValue, "<private>");
  return sanitized.slice(0, 64 * 1024);
};

export const writeSnapshot = (terminal: TestTerminal, fixture: Fixture, name: string): void => {
  if (!/^[a-z0-9-]+$/.test(name)) throw new Error(`unsafe snapshot name ${JSON.stringify(name)}`);
  const directory = path.join(artifactRoot, "snapshots");
  fs.mkdirSync(directory, { recursive: true, mode: 0o700 });
  fs.writeFileSync(path.join(directory, `${name}.txt`), sanitizeSnapshot(terminal.serialize().view, fixture), { encoding: "utf8", mode: 0o600 });
};

export const waitForFile = async (file: string, timeoutMs = 5_000): Promise<void> => {
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    if (fs.existsSync(file)) return;
    await new Promise((resolve) => setTimeout(resolve, 20));
  }
  throw new Error(`file did not appear within the bounded wait: ${file}`);
};

export const runNonTTY = (fixture: Fixture, args: string[]) => spawnSync(binary, args, {
  cwd: fixture.repo,
  env: fixture.env,
  encoding: "utf8",
  timeout: 15_000
});

import { spawn, spawnSync } from "node:child_process";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import process from "node:process";
import { fileURLToPath } from "node:url";

const moduleDirectory = path.dirname(fileURLToPath(import.meta.url));
const packageRoot = [
  process.cwd(),
  path.resolve(moduleDirectory, ".."),
  path.resolve(moduleDirectory, "..", ".."),
  path.resolve(moduleDirectory, "..", "..", "..")
].find((candidate) => fs.existsSync(path.join(candidate, "package.json")));
if (!packageRoot) throw new Error("cannot locate the TUI QA package root");
const repositoryRoot = path.resolve(process.env.AM_TUI_QA_REPO_ROOT ?? path.join(packageRoot, "..", ".."));
export const binary = path.resolve(process.env.AM_TUI_QA_BINARY ?? path.join(repositoryRoot, ".local", "agent-marketplace", "bin", "agent-marketplace-tui-qa"));
export const artifactRoot = path.resolve(process.env.AM_TUI_QA_ARTIFACTS ?? path.join(packageRoot, "test-results"));

export interface ProcessFixture {
  root: string;
  home: string;
  repo: string;
  env: Record<string, string>;
  cleanup(): void;
}

export const readRegularFile = (file: string): Buffer => {
  const fd = fs.openSync(file, fs.constants.O_RDONLY | fs.constants.O_NOFOLLOW | fs.constants.O_NONBLOCK);
  try {
    if (!fs.fstatSync(fd).isFile()) throw new Error(`not a regular file: ${file}`);
    return fs.readFileSync(fd);
  } finally {
    fs.closeSync(fd);
  }
};

export const write = (file: string, content: string, mode = 0o600): void => {
  fs.mkdirSync(path.dirname(file), { recursive: true, mode: 0o700 });
  fs.writeFileSync(file, content, { encoding: "utf8", mode });
};

// organizationConfig optionally redirects the fixed organization config path
// (under the e2e build tag) at a fixture-owned file. When omitted the layer
// reads as absent, which is what the default journeys expect.
export const isolatedEnvironment = (
  root: string,
  home: string,
  tmp: string,
  xdg: string,
  organizationConfig?: string
): Record<string, string> => ({
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
  AGENT_MARKETPLACE_V3_ORG_CONFIG: organizationConfig ?? path.join(root, "missing-organization-config.yaml"),
  TERM: "xterm-256color",
  LANG: "C.UTF-8"
});

export const createProcessFixture = (name: string): ProcessFixture => {
  if (!/^[a-z0-9-]+$/.test(name)) throw new Error(`unsafe fixture name ${JSON.stringify(name)}`);
  if (!fs.existsSync(binary)) throw new Error(`TUI QA binary not found: ${binary}`);
  const root = fs.mkdtempSync(path.join(os.tmpdir(), `agent-marketplace-process-${name}-`));
  try {
    const home = path.join(root, "home");
    const repo = path.join(root, "workspace");
    const tmp = path.join(root, "tmp");
    const xdg = path.join(root, "xdg");
    for (const directory of [home, repo, tmp, xdg]) fs.mkdirSync(directory, { recursive: true, mode: 0o700 });
    return {
      root,
      home,
      repo,
      env: isolatedEnvironment(root, home, tmp, xdg),
      cleanup: () => fs.rmSync(root, { recursive: true, force: true })
    };
  } catch (error) {
    fs.rmSync(root, { recursive: true, force: true });
    throw error;
  }
};

export const runNonTTY = (fixture: Pick<ProcessFixture, "repo" | "env">, args: string[], timeout = 5_000) => spawnSync(binary, args, {
  cwd: fixture.repo,
  env: fixture.env,
  encoding: "utf8",
  timeout
});

export interface AMResult {
  status: number | null;
  stdout: string;
  stderr: string;
}

// runAM runs the binary without blocking the Node event loop. The synchronous
// runner above freezes the loop for the whole child lifetime, which starves any
// fixture HTTP server (the organization marketplace stub) of requests; use this
// runner whenever the invoked command may talk to a fixture server.
export const runAM = async (fixture: Pick<ProcessFixture, "repo" | "env">, args: string[], timeoutMs = 30_000): Promise<AMResult> => {
  const child = spawn(binary, args, { cwd: fixture.repo, env: fixture.env, stdio: ["ignore", "pipe", "pipe"] });
  let stdout = "";
  let stderr = "";
  child.stdout.on("data", (chunk) => {
    stdout += String(chunk);
  });
  child.stderr.on("data", (chunk) => {
    stderr += String(chunk);
  });
  const timer = setTimeout(() => {
    child.kill("SIGKILL");
  }, timeoutMs);
  let status: number | null;
  try {
    status = await new Promise<number | null>((resolve, reject) => {
      child.once("error", reject);
      child.once("close", (code) => resolve(code));
    });
  } finally {
    clearTimeout(timer);
  }
  return { status, stdout, stderr };
};

export const targetPID = async (fixture: { pidFile: string }, timeoutMs = 5_000): Promise<number> => {
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    try {
      const pid = Number(fs.readFileSync(fixture.pidFile, "utf8").trim());
      if (Number.isSafeInteger(pid) && pid > 1) return pid;
    } catch (error) {
      if ((error as NodeJS.ErrnoException).code !== "ENOENT") throw error;
    }
    await new Promise((resolve) => setTimeout(resolve, 10));
  }
  throw new Error("real TUI process did not publish its PID within the bounded wait");
};

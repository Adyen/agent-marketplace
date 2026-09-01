import fs from "node:fs";
import os from "node:os";
import path from "node:path";

import { compareStrings } from "./order.js";

export interface Sandbox {
  root: string;
  home: string;
  xdgConfig: string;
  xdgCache: string;
  xdgData: string;
  xdgState: string;
  tmp: string;
  goCache: string;
  goModCache: string;
  goPath: string;
  npmCache: string;
  workspace: string;
  repo: string;
  registries: string;
  bin: string;
  editor: string;
  git: string;
  gitConfig: string;
  npmConfig: string;
  gnupg: string;
  logs: string;
  backups: string;
  ownership: string;
  generated: string;
  processEvidence: string;
  artifacts: string;
}

const safeSegment = (name: string, value: string): string => {
  if (!/^[A-Za-z0-9._-]{1,100}$/.test(value)) throw new Error(`${name} must be 1-100 safe path characters`);
  return value;
};

export const provision = (runId: string, flowId: string, attempt: number, base = os.tmpdir()): Sandbox => {
  safeSegment("runId", runId);
  safeSegment("flowId", flowId);
  if (!Number.isSafeInteger(attempt) || attempt < 1 || attempt > 1_000) throw new Error("attempt must be between 1 and 1000");
  const resolvedBase = fs.realpathSync(base);
  const root = fs.mkdtempSync(path.join(resolvedBase, `tui-qa-${runId}-${flowId}-a${attempt}-`));
  const bin = path.join(root, "bin");
  const sandbox: Sandbox = {
    root,
    home: path.join(root, "home"),
    xdgConfig: path.join(root, "xdg", "config"),
    xdgCache: path.join(root, "xdg", "cache"),
    xdgData: path.join(root, "xdg", "data"),
    xdgState: path.join(root, "xdg", "state"),
    tmp: path.join(root, "tmp"),
    goCache: path.join(root, "go", "build"),
    goModCache: path.join(root, "go", "mod"),
    goPath: path.join(root, "go", "path"),
    npmCache: path.join(root, "npm", "cache"),
    workspace: path.join(root, "workspace"),
    repo: path.join(root, "workspace", "repo"),
    registries: path.join(root, "registries"),
    bin,
    editor: path.join(bin, "editor"),
    git: path.join(bin, "git"),
    gitConfig: path.join(root, "gitconfig"),
    npmConfig: path.join(root, "npmrc"),
    gnupg: path.join(root, "gnupg"),
    logs: path.join(root, "logs"),
    backups: path.join(root, "backups"),
    ownership: path.join(root, "ownership"),
    generated: path.join(root, "generated"),
    processEvidence: path.join(root, "process"),
    artifacts: path.join(root, "artifacts")
  };
  for (const directory of [
    sandbox.home,
    sandbox.xdgConfig,
    sandbox.xdgCache,
    sandbox.xdgData,
    sandbox.xdgState,
    sandbox.tmp,
    sandbox.goCache,
    sandbox.goModCache,
    sandbox.goPath,
    sandbox.npmCache,
    sandbox.repo,
    sandbox.registries,
    sandbox.bin,
    sandbox.gnupg,
    sandbox.logs,
    sandbox.backups,
    sandbox.ownership,
    sandbox.generated,
    sandbox.processEvidence,
    sandbox.artifacts
  ]) {
    fs.mkdirSync(directory, { recursive: true, mode: 0o700 });
  }
  return sandbox;
};

const checkedExitCode = (exitCode: number): number => {
  if (!Number.isSafeInteger(exitCode) || exitCode < 0 || exitCode > 255) throw new Error("editor exit code must be between 0 and 255");
  return exitCode;
};

export const writeControlledGit = (gitPath: string): void => {
  const systemGit = "/usr/bin/git";
  if (!fs.existsSync(systemGit)) throw new Error(`controlled Git binary missing: ${systemGit}`);
  fs.mkdirSync(path.dirname(gitPath), { recursive: true });
  fs.writeFileSync(
    gitPath,
    `#!/bin/sh\nexec ${systemGit} -c core.hooksPath=/dev/null -c credential.helper= -c commit.gpgSign=false "$@"\n`,
    { encoding: "utf8", mode: 0o755 }
  );
};

export const writeFakeEditor = (editorPath: string, exitCode: number): void => {
  fs.mkdirSync(path.dirname(editorPath), { recursive: true });
  fs.writeFileSync(
    editorPath,
    `#!/bin/sh\numask 077\nif [ "\${AM_TUI_QA_EDITOR_SPAWN_CHILD:-0}" = "1" ]; then\n  detached=0\n  if command -v setsid >/dev/null 2>&1; then\n    setsid sh -c 'trap "" HUP; exec sleep 300' </dev/null >/dev/null 2>&1 &\n    detached=1\n  else\n    ( trap '' HUP; exec sleep 300 ) </dev/null >/dev/null 2>&1 &\n  fi\n  if [ -n "\${AM_TUI_QA_DESCENDANT_PID_FILE:-}" ]; then\n    child_pid=$!\n    child_pgid=\n    attempts=0\n    while [ "$attempts" -lt 100 ]; do\n      child_pgid=$(ps -o pgid= -p "$child_pid" | tr -d ' ')\n      if [ -n "$child_pgid" ] && { [ "$detached" -eq 0 ] || [ "$child_pgid" = "$child_pid" ]; }; then break; fi\n      attempts=$((attempts + 1))\n      sleep 0.01\n    done\n    if [ -z "$child_pgid" ] || { [ "$detached" -eq 1 ] && [ "$child_pgid" != "$child_pid" ]; }; then kill "$child_pid"; exit 125; fi\n    identity_tmp="$AM_TUI_QA_DESCENDANT_PID_FILE.tmp.$$"\n    printf '{"pid":%s,"pgid":%s}\\n' "$child_pid" "$child_pgid" > "$identity_tmp"\n    mv "$identity_tmp" "$AM_TUI_QA_DESCENDANT_PID_FILE"\n  fi\nfi\nexit ${checkedExitCode(exitCode)}\n`,
    { encoding: "utf8", mode: 0o755 }
  );
};

export const environmentForTest = (sandbox: Sandbox): NodeJS.ProcessEnv => {
  writeFakeEditor(sandbox.editor, 0);
  writeControlledGit(sandbox.git);
  fs.writeFileSync(sandbox.gitConfig, "", { encoding: "utf8", mode: 0o600 });
  fs.writeFileSync(sandbox.npmConfig, "", { encoding: "utf8", mode: 0o600 });
  return {
    PATH: `${sandbox.bin}${path.delimiter}/usr/bin${path.delimiter}/bin`,
    HOME: sandbox.home,
    USER: "tui-qa",
    LOGNAME: "tui-qa",
    XDG_CONFIG_HOME: sandbox.xdgConfig,
    XDG_CACHE_HOME: sandbox.xdgCache,
    XDG_DATA_HOME: sandbox.xdgData,
    XDG_STATE_HOME: sandbox.xdgState,
    TMPDIR: sandbox.tmp,
    TMP: sandbox.tmp,
    TEMP: sandbox.tmp,
    GOCACHE: sandbox.goCache,
    GOMODCACHE: sandbox.goModCache,
    GOPATH: sandbox.goPath,
    NPM_CONFIG_CACHE: sandbox.npmCache,
    NPM_CONFIG_USERCONFIG: sandbox.npmConfig,
    GNUPGHOME: sandbox.gnupg,
    EDITOR: sandbox.editor,
    VISUAL: sandbox.editor,
    GIT_EDITOR: sandbox.editor,
    GIT_CONFIG_NOSYSTEM: "1",
    GIT_CONFIG_SYSTEM: "/dev/null",
    GIT_CONFIG_GLOBAL: sandbox.gitConfig,
    GIT_CONFIG_COUNT: "4",
    GIT_CONFIG_KEY_0: "core.hooksPath",
    GIT_CONFIG_VALUE_0: "/dev/null",
    GIT_CONFIG_KEY_1: "credential.helper",
    GIT_CONFIG_VALUE_1: "",
    GIT_CONFIG_KEY_2: "protocol.file.allow",
    GIT_CONFIG_VALUE_2: "always",
    GIT_CONFIG_KEY_3: "commit.gpgSign",
    GIT_CONFIG_VALUE_3: "false",
    GIT_ASKPASS: "/bin/false",
    GIT_TERMINAL_PROMPT: "0",
    GIT_SSH_COMMAND: "/bin/false",
    SSH_ASKPASS: "/bin/false",
    SSH_ASKPASS_REQUIRE: "never",
    CODEX_HOME: path.join(sandbox.home, ".codex"),
    AGENT_MARKETPLACE_LOG_ENABLED: "true",
    AGENT_MARKETPLACE_LOG_FILE: path.join(sandbox.logs, "agent-marketplace.log"),
    AM_TUI_QA_PROCESS_EVIDENCE_DIR: sandbox.processEvidence,
    AM_TUI_QA_DESCENDANT_PID_FILE: path.join(sandbox.processEvidence, "descendant.pid"),
    TERM: "xterm-256color",
    LANG: "C.UTF-8"
  };
};

export interface TreeSummary {
  files: Record<string, { size: number; mode: number; digest: string }>;
  symlinks: Record<string, string>;
  truncated: boolean;
}

const SUMMARY_MAX_ENTRIES = 1_000;
const SUMMARY_MAX_BYTES = 10 * 1024 * 1024;

const within = (candidate: string, root: string): boolean => {
  const relative = path.relative(root, candidate);
  return relative === "" || (!path.isAbsolute(relative) && relative !== ".." && !relative.startsWith(`..${path.sep}`));
};

const resolveSymlinkTarget = (linkPath: string): string => {
  const target = fs.readlinkSync(linkPath);
  const lexicalTarget = path.resolve(fs.realpathSync(path.dirname(linkPath)), target);
  return fs.existsSync(linkPath) ? fs.realpathSync(linkPath) : lexicalTarget;
};

export const summarize = (directory: string, digest: (file: string) => string, allowedRoot = directory): TreeSummary => {
  const files: TreeSummary["files"] = {};
  const symlinks: TreeSummary["symlinks"] = {};
  if (!fs.existsSync(directory)) return { files, symlinks, truncated: false };
  const resolvedRoot = fs.realpathSync(allowedRoot);
  const resolvedDirectory = fs.realpathSync(directory);
  if (!within(resolvedDirectory, resolvedRoot)) throw new Error(`summary root ${resolvedDirectory} leaves ${resolvedRoot}`);
  let entries = 0;
  let totalBytes = 0;
  let truncated = false;

  const walk = (current: string): void => {
    for (const entry of fs.readdirSync(current, { withFileTypes: true }).sort((a, b) => compareStrings(a.name, b.name))) {
      if (truncated) return;
      entries++;
      if (entries > SUMMARY_MAX_ENTRIES) {
        truncated = true;
        return;
      }
      const full = path.join(current, entry.name);
      const stat = fs.lstatSync(full);
      if (stat.isSymbolicLink()) {
        const target = resolveSymlinkTarget(full);
        if (!within(target, resolvedRoot)) throw new Error(`symlink escape rejected: ${full} -> ${target}`);
        symlinks[path.relative(directory, full)] = path.relative(resolvedRoot, target);
      } else if (stat.isDirectory()) {
        walk(full);
      } else if (stat.isFile()) {
        if (totalBytes + stat.size > SUMMARY_MAX_BYTES) {
          truncated = true;
          return;
        }
        files[path.relative(directory, full)] = { size: stat.size, mode: stat.mode & 0o7777, digest: digest(full) };
        totalBytes += stat.size;
      }
    }
  };
  walk(directory);
  return { files, symlinks, truncated };
};

export const rejectTreeSymlinkEscapes = (treeRoot: string, allowedRoot: string): void => {
  if (!fs.existsSync(treeRoot)) return;
  const resolvedAllowed = fs.realpathSync(allowedRoot);
  let entries = 0;
  const walk = (current: string): void => {
    for (const entry of fs.readdirSync(current, { withFileTypes: true })) {
      if (++entries > 10_000) throw new Error("symlink validation entry limit exceeded");
      const full = path.join(current, entry.name);
      const stat = fs.lstatSync(full);
      if (stat.isSymbolicLink()) {
        const target = resolveSymlinkTarget(full);
        if (!within(target, resolvedAllowed)) throw new Error(`symlink escape rejected: ${full} -> ${target}`);
      } else if (stat.isDirectory()) {
        walk(full);
      }
    }
  };
  walk(treeRoot);
};

export const rejectSymlinkEscape = (candidate: string, allowedRoot: string): void => {
  const resolvedRoot = fs.realpathSync(allowedRoot);
  let ancestor = candidate;
  while (!fs.existsSync(ancestor)) {
    const parent = path.dirname(ancestor);
    if (parent === ancestor) throw new Error(`symlink escape rejected: no existing ancestor for ${candidate}`);
    ancestor = parent;
  }
  const resolvedAncestor = fs.realpathSync(ancestor);
  const resolvedCandidate = path.join(resolvedAncestor, path.relative(ancestor, candidate));
  if (!within(resolvedCandidate, resolvedRoot)) {
    throw new Error(`symlink escape rejected: ${candidate} resolves to ${resolvedCandidate}, outside ${resolvedRoot}`);
  }
  let current = resolvedRoot;
  for (const part of path.relative(resolvedRoot, resolvedCandidate).split(path.sep).filter(Boolean)) {
    current = path.join(current, part);
    if (!fs.existsSync(current)) continue;
    if (fs.lstatSync(current).isSymbolicLink()) {
      const target = fs.realpathSync(current);
      if (!within(target, resolvedRoot)) throw new Error(`symlink escape rejected: ${current} -> ${target} leaves ${resolvedRoot}`);
    }
  }
};

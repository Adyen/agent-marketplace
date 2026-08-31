import assert from "node:assert/strict";
import { execFileSync, spawnSync } from "node:child_process";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import process from "node:process";
import { describe, it, type TestContext } from "node:test";

import { sha256File } from "../../src/digest.js";
import {
  environmentForTest,
  provision,
  rejectSymlinkEscape,
  rejectTreeSymlinkEscapes,
  summarize,
  writeFakeEditor
} from "../../src/sandbox.js";

const temporaryDirectory = (t: TestContext, prefix: string): string => {
  const directory = fs.mkdtempSync(path.join(os.tmpdir(), prefix));
  t.after(() => fs.rmSync(directory, { recursive: true, force: true }));
  return directory;
};

const sandboxFor = (t: TestContext, flow: string, attempt = 1) => {
  const sandbox = provision("unit", flow, attempt);
  t.after(() => fs.rmSync(sandbox.root, { recursive: true, force: true }));
  return sandbox;
};

describe("provision", () => {
  it("creates a unique complete sandbox per attempt", (t) => {
    const first = sandboxFor(t, "flow", 1);
    const second = sandboxFor(t, "flow", 2);
    assert.notEqual(first.root, second.root);
    for (const key of [
      "home",
      "xdgConfig",
      "xdgCache",
      "xdgData",
      "xdgState",
      "tmp",
      "goCache",
      "goModCache",
      "goPath",
      "npmCache",
      "repo",
      "registries",
      "bin",
      "gnupg",
      "logs",
      "backups",
      "ownership",
      "generated",
      "processEvidence",
      "artifacts"
    ] as const) {
      assert.ok(fs.statSync(first[key]).isDirectory(), key);
      assert.ok(first[key].startsWith(`${first.root}${path.sep}`), key);
    }
  });

  it("rejects unsafe identities and attempt numbers", () => {
    assert.throws(() => provision("../run", "flow", 1), /runId/);
    assert.throws(() => provision("run", "flow/name", 1), /flowId/);
    assert.throws(() => provision("run", "flow", 0), /attempt/);
  });
});

describe("environmentForTest", () => {
  it("isolates application paths and neutralizes host integration", (t) => {
    const forbidden = [
      "HTTP_PROXY",
      "HTTPS_PROXY",
      "ALL_PROXY",
      "NO_PROXY",
      "http_proxy",
      "https_proxy",
      "SSH_AUTH_SOCK",
      "SSH_AGENT_PID",
      "AWS_ACCESS_KEY_ID",
      "GITHUB_TOKEN",
      "NODE_AUTH_TOKEN",
      "NPM_TOKEN",
      "GIT_DIR",
      "GIT_WORK_TREE",
      "GIT_CONFIG_PARAMETERS",
      "GIT_HTTP_EXTRA_HEADER",
      "NODE_OPTIONS"
    ];
    const original = new Map(forbidden.map((key) => [key, process.env[key]]));
    for (const key of forbidden) process.env[key] = "poison";
    t.after(() => {
      for (const [key, value] of original) {
        if (value === undefined) delete process.env[key];
        else process.env[key] = value;
      }
    });
    const sandbox = sandboxFor(t, "environment");
    const env = environmentForTest(sandbox);
    assert.equal(env.HOME, sandbox.home);
    assert.equal(env.XDG_CONFIG_HOME, sandbox.xdgConfig);
    assert.equal(env.XDG_CACHE_HOME, sandbox.xdgCache);
    assert.equal(env.XDG_DATA_HOME, sandbox.xdgData);
    assert.equal(env.XDG_STATE_HOME, sandbox.xdgState);
    assert.equal(env.GOCACHE, sandbox.goCache);
    assert.equal(env.GOMODCACHE, sandbox.goModCache);
    assert.equal(env.GOPATH, sandbox.goPath);
    assert.equal(env.NPM_CONFIG_CACHE, sandbox.npmCache);
    assert.equal(env.NPM_CONFIG_USERCONFIG, sandbox.npmConfig);
    assert.equal(env.GIT_CONFIG_SYSTEM, "/dev/null");
    assert.equal(env.GIT_CONFIG_GLOBAL, sandbox.gitConfig);
    assert.equal(env.GIT_ASKPASS, "/bin/false");
    assert.equal(env.GIT_SSH_COMMAND, "/bin/false");
    assert.equal(env.SSH_ASKPASS, "/bin/false");
    assert.equal(env.EDITOR, sandbox.editor);
    assert.equal(env.CODEX_HOME, path.join(sandbox.home, ".codex"));
    assert.equal(env.PATH, `${sandbox.bin}${path.delimiter}/usr/bin${path.delimiter}/bin`);
    for (const key of forbidden) assert.equal(env[key], undefined, key);
    assert.match(execFileSync("git", ["--version"], { cwd: sandbox.repo, env, encoding: "utf8" }), /^git version /);
  });

  it("uses only executable controlled shims", (t) => {
    const sandbox = sandboxFor(t, "shims");
    const env = environmentForTest(sandbox);
    assert.ok(fs.statSync(sandbox.git).mode & 0o111);
    assert.ok(fs.statSync(sandbox.editor).mode & 0o111);
    writeFakeEditor(sandbox.editor, 3);
    assert.equal(spawnSync(sandbox.editor, [], { cwd: sandbox.repo, env }).status, 3);
    assert.throws(() => writeFakeEditor(sandbox.editor, 256), /exit code/);
  });
});

describe("summarize", () => {
  it("captures bounded digests, modes, and contained symlinks", (t) => {
    const directory = temporaryDirectory(t, "tui-qa-summary-");
    fs.mkdirSync(path.join(directory, "a"));
    const file = path.join(directory, "a", "x.txt");
    fs.writeFileSync(file, "content", { mode: 0o640 });
    fs.symlinkSync(file, path.join(directory, "inside"));
    const summary = summarize(directory, sha256File);
    assert.deepEqual(summary.files["a/x.txt"], { size: 7, mode: 0o640, digest: sha256File(file) });
    assert.equal(summary.symlinks.inside, "a/x.txt");
    assert.equal(summary.truncated, false);
  });

  it("marks entry and byte limits instead of reading beyond them", (t) => {
    const entries = temporaryDirectory(t, "tui-qa-entries-");
    for (let index = 0; index < 1_001; index++) fs.writeFileSync(path.join(entries, `${index}.txt`), "x");
    assert.equal(summarize(entries, sha256File).truncated, true);

    const bytes = temporaryDirectory(t, "tui-qa-bytes-");
    const large = path.join(bytes, "large.bin");
    fs.writeFileSync(large, "");
    fs.truncateSync(large, 10 * 1024 * 1024 + 1);
    assert.deepEqual(summarize(bytes, sha256File), { files: {}, symlinks: {}, truncated: true });
  });

  it("returns an empty summary for a missing directory", (t) => {
    const root = temporaryDirectory(t, "tui-qa-missing-");
    assert.deepEqual(summarize(path.join(root, "missing"), sha256File), { files: {}, symlinks: {}, truncated: false });
  });
});

describe("symlink boundaries", () => {
  it("allows links that remain inside the sandbox", (t) => {
    const root = temporaryDirectory(t, "tui-qa-link-");
    fs.mkdirSync(path.join(root, "cache"));
    fs.mkdirSync(path.join(root, "workspace"));
    fs.symlinkSync(path.join(root, "cache"), path.join(root, "workspace", "inside"));
    fs.symlinkSync("missing", path.join(root, "workspace", "dangling"));
    rejectSymlinkEscape(path.join(root, "workspace", "inside", "file"), root);
    rejectSymlinkEscape(path.join(root, "workspace"), path.parse(root).root);
    rejectTreeSymlinkEscapes(path.join(root, "workspace"), root);
    assert.equal(summarize(path.join(root, "workspace"), sha256File, root).symlinks.dangling, "workspace/missing");
  });

  it("rejects lexical and symlink escapes", (t) => {
    const root = temporaryDirectory(t, "tui-qa-link-");
    const outside = temporaryDirectory(t, "tui-qa-outside-");
    fs.symlinkSync(outside, path.join(root, "escape"));
    fs.symlinkSync(path.join(outside, "missing"), path.join(root, "dangling-escape"));
    assert.throws(() => rejectSymlinkEscape(path.join(root, "escape", "file"), root), /symlink escape rejected/);
    assert.throws(() => rejectTreeSymlinkEscapes(root, root), /symlink escape rejected/);
    assert.throws(() => summarize(root, sha256File), /symlink escape rejected/);
    assert.throws(() => rejectSymlinkEscape(path.join(outside, "file"), root), /outside/);
  });
});

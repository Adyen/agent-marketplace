import { execFileSync } from "node:child_process";
import fs from "node:fs";
import path from "node:path";

import { sha256File } from "./digest.js";

// Release-equivalent build identity for the QA binary. The recipe is fixed:
// Go version from go.mod, CGO_ENABLED=0, no test build tags, no race or
// coverage instrumentation, and only the normal version ldflag.

export interface BuildIdentity {
  path: string;
  sha256: string;
  buildClass: "release-equivalent" | "instrumented";
  cgoEnabled: boolean;
  goVersion: string;
  goos: string;
  goarch: string;
  command: string[];
  tags: string[];
  ldflags: string[];
}

export const goVersion = (cwd: string, env: NodeJS.ProcessEnv = process.env): string =>
  execFileSync("go", ["version"], { cwd, env, encoding: "utf8" }).trim().replace(/^go version /, "").replace(/ .+$/, "");

export const goModVersion = (cwd: string): string => {
  const mod = fs.readFileSync(path.join(cwd, "go.mod"), "utf8");
  const match = mod.match(/^go\s+(\S+)/m);
  if (!match) {
    throw new Error("cannot read Go version from go.mod");
  }
  return `go${match[1]}`;
};

const goVersionParts = (value: string): [number, number, number] => {
  const match = value.match(/^go(\d+)\.(\d+)(?:\.(\d+))?$/);
  if (!match) throw new Error(`invalid Go version ${JSON.stringify(value)}`);
  return [Number(match[1]), Number(match[2]), Number(match[3] ?? 0)];
};

export const goVersionAtLeast = (actual: string, required: string): boolean => {
  const actualParts = goVersionParts(actual);
  const requiredParts = goVersionParts(required);
  for (let index = 0; index < actualParts.length; index++) {
    if (actualParts[index] !== requiredParts[index]) return actualParts[index] > requiredParts[index];
  }
  return true;
};

/**
 * buildReleaseEquivalent compiles the production binary with the fixed
 * release-equivalent recipe. Instrumentation flags (tags/race/cover) are
 * never accepted here; callers that need them must use buildInstrumented
 * and accept the instrumented build class.
 */
export const buildReleaseEquivalent = (repoRoot: string, outPath: string, headSha: string): BuildIdentity => {
  if (!/^[0-9a-f]{7,64}$/.test(headSha)) throw new Error("head SHA must be lowercase hexadecimal");
  const buildEnv: NodeJS.ProcessEnv = {
    ...process.env,
    CGO_ENABLED: "0",
    GOENV: "off",
    GOFLAGS: "",
    GOTOOLCHAIN: process.env.GOTOOLCHAIN ?? "auto"
  };
  const requiredGoVersion = goModVersion(repoRoot);
  const actualGoVersion = goVersion(repoRoot, buildEnv);
  if (!goVersionAtLeast(actualGoVersion, requiredGoVersion)) {
    throw new Error(`release-equivalent build requires at least ${requiredGoVersion}, found ${actualGoVersion}`);
  }
  const version = `tui-qa-${headSha}`;
  const ldflags = [`-X main.version=${version}`];
  fs.mkdirSync(path.dirname(outPath), { recursive: true });
  if (fs.existsSync(outPath)) throw new Error(`refusing to overwrite build output: ${outPath}`);
  const args = [
    "build",
    "-o",
    outPath,
    "-ldflags",
    ldflags[0],
    "./cmd/agent-marketplace"
  ];
  execFileSync("go", args, {
    cwd: repoRoot,
    stdio: ["ignore", "pipe", "inherit"],
    env: buildEnv
  });
  const goos = buildEnv.GOOS || execFileSync("go", ["env", "GOOS"], { cwd: repoRoot, env: buildEnv, encoding: "utf8" }).trim();
  const goarch = buildEnv.GOARCH || execFileSync("go", ["env", "GOARCH"], { cwd: repoRoot, env: buildEnv, encoding: "utf8" }).trim();
  return {
    path: outPath,
    sha256: sha256File(outPath),
    buildClass: "release-equivalent",
    cgoEnabled: false,
    goVersion: actualGoVersion,
    goos,
    goarch,
    command: ["go", ...args],
    tags: [],
    ldflags
  };
};

/** buildInstrumented explicitly labels a non-release binary (e2e tags, race, coverage, fault injection). */
export const instrumentedIdentity = (
  outPath: string,
  compilerVersion: string,
  goos: string,
  goarch: string,
  command: string[],
  tags: string[] = [],
  ldflags: string[] = [],
  cgoEnabled = false
): BuildIdentity => {
  goVersionParts(compilerVersion);
  if (command.length === 0) throw new Error("instrumented build command must not be empty");
  return {
    path: outPath,
    sha256: sha256File(outPath),
    buildClass: "instrumented",
    cgoEnabled,
    goVersion: compilerVersion,
    goos,
    goarch,
    command: [...command],
    tags: [...tags],
    ldflags: [...ldflags]
  };
};

export const readBuildIdentity = (manifestPath: string, binaryPath: string): BuildIdentity => {
  const value: unknown = JSON.parse(fs.readFileSync(manifestPath, "utf8"));
  if (typeof value !== "object" || value === null) throw new Error("build identity must be an object");
  const parsed = value as Partial<BuildIdentity>;
  const requiredStrings: Array<keyof BuildIdentity> = ["path", "sha256", "buildClass", "goVersion", "goos", "goarch"];
  for (const key of requiredStrings) {
    if (typeof parsed[key] !== "string" || parsed[key] === "") throw new Error(`build identity ${String(key)} missing`);
  }
  if (parsed.buildClass !== "release-equivalent" && parsed.buildClass !== "instrumented") {
    throw new Error("build identity buildClass invalid");
  }
  if (typeof parsed.cgoEnabled !== "boolean") throw new Error("build identity cgoEnabled invalid");
  for (const key of ["command", "tags", "ldflags"] as const) {
    if (!Array.isArray(parsed[key]) || !parsed[key]!.every((value) => typeof value === "string")) {
      throw new Error(`build identity ${key} invalid`);
    }
  }
  if (parsed.command!.length === 0) throw new Error("build identity command invalid");
  goVersionParts(parsed.goVersion!);
  const digest = sha256File(binaryPath);
  if (parsed.sha256 !== digest) throw new Error(`build identity digest mismatch: manifest ${String(parsed.sha256)}, actual ${digest}`);
  return {
    path: binaryPath,
    sha256: digest,
    buildClass: parsed.buildClass,
    cgoEnabled: parsed.cgoEnabled,
    goVersion: parsed.goVersion!,
    goos: parsed.goos!,
    goarch: parsed.goarch!,
    command: [...parsed.command!],
    tags: [...parsed.tags!],
    ldflags: [...parsed.ldflags!]
  };
};

/** headSha resolves the current HEAD; used only to stamp the QA binary version. */
export const headSha = (cwd: string): string => {
  const value = execFileSync("git", ["rev-parse", "HEAD"], { cwd, encoding: "utf8" }).trim();
  if (!/^[0-9a-f]{40,64}$/.test(value)) throw new Error(`invalid HEAD revision ${JSON.stringify(value)}`);
  return value;
};

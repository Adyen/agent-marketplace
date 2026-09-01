import type { BuildIdentity } from "./build.js";
import type { FlowStatus } from "./status.js";

export type AttemptTerminalStatus = "PASS" | "FAIL" | "BLOCKED";

export const aggregateAttemptStatuses = (statuses: AttemptTerminalStatus[]): FlowStatus => {
  if (statuses.length === 0 || statuses.length > 2) throw new Error("workbench flows require one or two attempts");
  const last = statuses.at(-1)!;
  if (statuses.length === 2 && last === "PASS") return "FLAKY";
  if (statuses.includes("FAIL")) return "FAIL";
  return last;
};

export const projectBuildIdentity = (identity: BuildIdentity, repoRoot: string): BuildIdentity => ({
  ...identity,
  path: "<run-binary>/agent-marketplace",
  command: identity.command.map((value, index) =>
    identity.command[index - 1] === "-o" ? "<run-binary>/agent-marketplace" : value.replaceAll(repoRoot, "<repo-root>"))
});

export const certifyingRun = (contained: boolean, worktreeDirty: boolean, identity: BuildIdentity): boolean =>
  contained && !worktreeDirty && identity.buildClass === "release-equivalent" && !identity.cgoEnabled;

export const isHarnessLaunchFailure = (output: string): boolean =>
  /spawn .*\b(?:EACCES|ENOENT)\b/.test(output) ||
  /pty-wrapper: (?:target separator missing|invalid options|target cwd unavailable|target missing)/.test(output);

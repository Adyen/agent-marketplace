import { execFileSync } from "node:child_process";
import fs from "node:fs";
import path from "node:path";
import process from "node:process";

export interface ProcessRow {
  pid: number;
  ppid: number;
  pgid: number;
  state: string;
}

export interface ProcessCleanupEvidence {
  rootProcess: number;
  processGroup: number;
  detected: number[];
  terminated: number[];
  remaining: number[];
  complete: boolean;
}

export const parseProcessTable = (output: string): ProcessRow[] => {
  const rows: ProcessRow[] = [];
  for (const line of output.split("\n")) {
    const match = line.trim().match(/^(\d+)\s+(\d+)\s+(\d+)\s+(\S+)/);
    if (!match) continue;
    rows.push({ pid: Number(match[1]), ppid: Number(match[2]), pgid: Number(match[3]), state: match[4] });
  }
  return rows;
};

export const processTable = (): ProcessRow[] => {
  if (process.platform === "win32") throw new Error("TUI QA process certification requires a Unix PTY");
  return parseProcessTable(execFileSync("ps", ["-axo", "pid=,ppid=,pgid=,state="], { encoding: "utf8" }));
};

export const processGroupMembers = (pgid: number, rows = processTable()): ProcessRow[] =>
  rows.filter((row) => row.pgid === pgid);

export const processTreeMembers = (rootPid: number, rows = processTable()): ProcessRow[] => {
  const descendants = new Set([rootPid]);
  let changed = true;
  while (changed) {
    changed = false;
    for (const row of rows) {
      if (descendants.has(row.ppid) && !descendants.has(row.pid)) {
        descendants.add(row.pid);
        changed = true;
      }
    }
  }
  return rows.filter((row) => descendants.has(row.pid));
};

const ownProcessGroup = (): number | undefined => processTable().find((row) => row.pid === process.pid)?.pgid;

const assertSafeProcess = (pid: number): void => {
  if (!Number.isSafeInteger(pid) || pid <= 1) throw new Error(`unsafe process id ${pid}`);
};

const assertSafeGroup = (pgid: number): void => {
  assertSafeProcess(pgid);
  if (pgid === ownProcessGroup()) throw new Error(`refusing to signal workbench process group ${pgid}`);
};

export const signalProcessGroup = (pgid: number, signal: NodeJS.Signals): void => {
  assertSafeGroup(pgid);
  try {
    process.kill(-pgid, signal);
  } catch (error) {
    if ((error as NodeJS.ErrnoException).code !== "ESRCH") throw error;
  }
};

export const signalProcess = (pid: number, signal: NodeJS.Signals): void => {
  assertSafeProcess(pid);
  try {
    process.kill(pid, signal);
  } catch (error) {
    if ((error as NodeJS.ErrnoException).code !== "ESRCH") throw error;
  }
};

const wait = (ms: number): Promise<void> => new Promise((resolve) => setTimeout(resolve, ms));

const sorted = (values: Iterable<number>): number[] => [...new Set(values)].sort((a, b) => a - b);

const remainingProcesses = (detected: Map<number, number>, pgid: number): number[] => {
  const rows = processTable();
  const current = new Set(processGroupMembers(pgid, rows).map((row) => row.pid));
  for (const row of rows) if (detected.get(row.pid) === row.pgid) current.add(row.pid);
  return sorted(current);
};

const waitForCleanup = async (detected: Map<number, number>, pgid: number, graceMs: number): Promise<number[]> => {
  const deadline = Date.now() + graceMs;
  let remaining = remainingProcesses(detected, pgid);
  while (remaining.length > 0 && Date.now() < deadline) {
    await wait(20);
    remaining = remainingProcesses(detected, pgid);
  }
  return remaining;
};

export const cleanupProcessTree = async (
  rootPid: number,
  pgid = rootPid,
  graceMs = 250
): Promise<ProcessCleanupEvidence> => {
  assertSafeProcess(rootPid);
  assertSafeGroup(pgid);
  const initial = processTable();
  const detectedRows = [
    ...new Map(
      [...processGroupMembers(pgid, initial), ...processTreeMembers(rootPid, initial)].map((row) => [row.pid, row])
    ).values()
  ];
  const detected = new Map(detectedRows.map((row) => [row.pid, row.pgid]));
  if (detected.size === 0) {
    return {
      rootProcess: rootPid,
      processGroup: pgid,
      detected: [],
      terminated: [],
      remaining: [],
      complete: true
    };
  }

  signalProcessGroup(pgid, "SIGTERM");
  for (const row of detectedRows) {
    if (row.pgid !== pgid) signalProcess(row.pid, "SIGTERM");
  }

  let remaining = await waitForCleanup(detected, pgid, graceMs);
  if (remaining.length > 0) {
    signalProcessGroup(pgid, "SIGKILL");
    for (const pid of remaining) {
      const row = processTable().find((candidate) => candidate.pid === pid);
      if (row && row.pgid === detected.get(pid) && row.pgid !== pgid) signalProcess(pid, "SIGKILL");
    }
    remaining = await waitForCleanup(detected, pgid, graceMs);
  }

  const detectedPids = sorted(detected.keys());
  return {
    rootProcess: rootPid,
    processGroup: pgid,
    detected: detectedPids,
    terminated: detectedPids.filter((pid) => !remaining.includes(pid)),
    remaining,
    complete: remaining.length === 0
  };
};

export const cleanupProcessGroup = async (pgid: number, graceMs = 250): Promise<ProcessCleanupEvidence> =>
  cleanupProcessTree(pgid, pgid, graceMs);

export const writeProcessCleanupEvidence = (directory: string, evidence: ProcessCleanupEvidence, label = "final"): string => {
  if (!/^[A-Za-z0-9._-]+$/.test(label)) throw new Error(`unsafe process evidence label ${label}`);
  fs.mkdirSync(directory, { recursive: true });
  const content = JSON.stringify(evidence, null, 2) + "\n";
  for (let collision = 0; collision < 1_000; collision++) {
    const suffix = collision === 0 ? "" : `-${collision + 1}`;
    const file = path.join(directory, `process-cleanup-${evidence.processGroup}-${label}${suffix}.json`);
    try {
      fs.writeFileSync(file, content, { encoding: "utf8", flag: "wx", mode: 0o600 });
      return file;
    } catch (error) {
      if ((error as NodeJS.ErrnoException).code !== "EEXIST") throw error;
    }
  }
  throw new Error(`too many process cleanup evidence collisions for group ${evidence.processGroup}`);
};

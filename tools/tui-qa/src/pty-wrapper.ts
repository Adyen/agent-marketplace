import { spawn, spawnSync } from "node:child_process";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import process from "node:process";

import { TARGET_EXIT_BOUNDARY_VERSION, TERMINAL_RESET_SEQUENCE } from "./protocol.js";

const separator = process.argv.indexOf("--");
const argv = separator >= 0 ? process.argv.slice(separator + 1) : process.argv.slice(2);
if (argv.length === 0) {
  process.stderr.write("pty-wrapper: target missing\n");
  process.exit(127);
}

const [target, ...args] = argv;
const lifecycleDir = process.env.AM_TUI_QA_LIFECYCLE_DIR;
if (lifecycleDir) fs.mkdirSync(lifecycleDir, { recursive: true });
const activePidFile = process.env.AM_TUI_QA_ACTIVE_PID_FILE;

const child = spawn(target, args, {
  cwd: process.cwd(),
  env: process.env,
  stdio: "inherit"
});

if (activePidFile) {
  fs.mkdirSync(path.dirname(activePidFile), { recursive: true });
  fs.writeFileSync(activePidFile, `${child.pid ?? 0}\n`, { encoding: "utf8", mode: 0o600 });
}

const receivedSignals: string[] = [];
for (const signal of ["SIGINT", "SIGTERM", "SIGHUP"] as const) {
  process.on(signal, () => {
    receivedSignals.push(signal);
    if (child.pid) {
      try {
        child.kill(signal);
      } catch {
        // The target may already have exited; lifecycle evidence records that.
      }
    }
  });
}

interface TermiosProbe {
  ok: boolean;
  canonical: boolean | null;
  echo: boolean | null;
  raw: string;
  error?: string;
}

const termios = (): TermiosProbe => {
  const result = spawnSync("stty", ["-a"], { encoding: "utf8", stdio: ["inherit", "pipe", "pipe"] });
  const raw = `${result.stdout ?? ""} ${result.stderr ?? ""}`.trim();
  if (result.error || result.status !== 0) {
    return {
      ok: false,
      canonical: null,
      echo: null,
      raw,
      error: result.error?.message ?? `stty exited ${result.status ?? "without status"}`
    };
  }
  const tokens = raw.replace(/[;\n]/g, " ").split(/\s+/).filter(Boolean);
  return {
    ok: true,
    canonical: tokens.includes("icanon") && !tokens.includes("-icanon"),
    echo: tokens.includes("echo") && !tokens.includes("-echo"),
    raw
  };
};

child.on("error", (error) => {
  process.stderr.write(`pty-wrapper: ${error.message}\n`);
});

child.on("close", (code, signal) => {
  const beforeReset = termios();
  fs.writeSync(
    process.stdout.fd,
    `\nAM_TUI_QA_TARGET_EXIT_BOUNDARY version=${TARGET_EXIT_BOUNDARY_VERSION} code=${code ?? "null"} signal=${signal ?? "none"}\n`
  );
  const resetApplied = true;
  spawnSync("stty", ["sane"], { stdio: ["inherit", "ignore", "ignore"] });
  fs.writeSync(process.stdout.fd, TERMINAL_RESET_SEQUENCE);
  const afterReset = termios();
  const lifecycle = {
    wrapperPid: process.pid,
    childPid: child.pid ?? null,
    exitCode: code,
    signal,
    receivedSignals,
    boundaryVersion: TARGET_EXIT_BOUNDARY_VERSION,
    targetAtExit: {
      probeOK: beforeReset.ok,
      canonical: beforeReset.canonical,
      echo: beforeReset.echo,
      ...(beforeReset.error ? { error: beforeReset.error } : {})
    },
    harnessRecovery: {
      applied: resetApplied,
      probeOK: afterReset.ok,
      canonical: afterReset.canonical,
      echo: afterReset.echo,
      ...(afterReset.error ? { error: afterReset.error } : {})
    },
    hostTerminalRecoveryClaimed: false
  };
  if (lifecycleDir) {
    const file = path.join(lifecycleDir, `lifecycle-${process.pid}.json`);
    fs.writeFileSync(file, JSON.stringify(lifecycle, null, 2) + "\n", { encoding: "utf8", flag: "wx", mode: 0o600 });
  }
  const targetExitCode = code !== null ? code : signal ? 128 + (os.constants.signals[signal] ?? 0) : 1;
  const finalExitCode = beforeReset.ok && afterReset.ok ? targetExitCode : 125;
  if (!beforeReset.ok || !afterReset.ok) {
    process.stderr.write(`pty-wrapper: termios probe failed before=${beforeReset.error ?? "unknown"} after=${afterReset.error ?? "unknown"}\n`);
  }
  fs.writeSync(
    process.stdout.fd,
    `\nAM_TUI_QA_HARNESS_RECOVERY probe=${afterReset.ok ? 1 : 0} canonical=${afterReset.canonical === true ? 1 : 0} echo=${afterReset.echo === true ? 1 : 0} applied=${resetApplied ? 1 : 0}\n`
  );
  process.exit(finalExitCode);
});

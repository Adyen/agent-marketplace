import { execFileSync, spawn } from "node:child_process";
import fs from "node:fs";

const pidFile = process.env.AM_TUI_QA_DESCENDANT_PID_FILE;
if (!pidFile) throw new Error("AM_TUI_QA_DESCENDANT_PID_FILE is required");

const child = spawn(process.execPath, ["-e", "process.on('SIGHUP',()=>{}); setInterval(()=>{},1000)"], {
  detached: true,
  env: process.env,
  stdio: "ignore"
});
child.unref();
const pgid = Number(execFileSync("ps", ["-o", "pgid=", "-p", String(child.pid)], { encoding: "utf8" }).trim());
if (!child.pid || !Number.isSafeInteger(pgid) || pgid <= 1) {
  if (child.pid) process.kill(child.pid, "SIGKILL");
  throw new Error("detached child identity unavailable");
}
const temporary = `${pidFile}.tmp-${process.pid}`;
fs.writeFileSync(temporary, JSON.stringify({ pid: child.pid, pgid }) + "\n", { flag: "wx", mode: 0o600 });
fs.renameSync(temporary, pidFile);
process.stdout.write("editor parent exited\n");

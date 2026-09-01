import { spawn } from "node:child_process";
import fs from "node:fs";
import process from "node:process";

const marker = process.argv[2];
if (!marker) throw new Error("marker path required");

const sleeper = ["-e", "setInterval(() => {}, 1000)"];
const grouped = spawn(process.execPath, sleeper, { stdio: "ignore" });
const detached = spawn(process.execPath, sleeper, { detached: true, stdio: "ignore" });
detached.unref();

fs.writeFileSync(marker, JSON.stringify({ root: process.pid, grouped: grouped.pid, detached: detached.pid }) + "\n", {
  encoding: "utf8",
  flag: "wx",
  mode: 0o600
});

setInterval(() => {}, 1000);

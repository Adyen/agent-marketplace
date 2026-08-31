import fs from "node:fs";

const allowed = new Set(["dist", "dist-tests", ".tui-test"]);
for (const target of process.argv.slice(2)) {
  if (!allowed.has(target)) throw new Error(`refusing to clean unexpected path ${target}`);
  fs.rmSync(target, { recursive: true, force: true });
}

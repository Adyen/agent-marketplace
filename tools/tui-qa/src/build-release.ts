import fs from "node:fs";
import process from "node:process";

import { buildReleaseEquivalent, headSha } from "./build.js";

const repoRoot = process.env.AM_TUI_QA_REPO_ROOT;
const output = process.env.AM_TUI_QA_BUILD_OUT;
const manifest = process.env.AM_TUI_QA_BUILD_MANIFEST;
if (!repoRoot || !output || !manifest) throw new Error("repository root, build output, and manifest are required");
const identity = buildReleaseEquivalent(repoRoot, output, headSha(repoRoot));
fs.writeFileSync(manifest, JSON.stringify({ ...identity, path: "/opt/agent-marketplace" }, null, 2) + "\n", {
  encoding: "utf8",
  flag: "wx",
  mode: 0o600
});

import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import fs from "node:fs";
import path from "node:path";
import { test } from "node:test";

import { artifactRoot, privateValues, repositoryRoot } from "./helpers.js";

const trackedFiles = (): string[] => execFileSync(
  "git",
  ["ls-files", "-z", "--cached", "--others", "--exclude-standard", "--", "tools/tui-qa", ".github/workflows/ci.yml", "Makefile", "AGENTS.md", "CODING_STANDARDS.md"],
  { cwd: repositoryRoot, encoding: "utf8" }
).split("\0").filter(Boolean);

const snapshotFiles = (): string[] => {
  const root = path.join(artifactRoot, "snapshots");
  if (!fs.existsSync(root)) return [];
  return fs.readdirSync(root).map((name) => path.join(root, name)).filter((file) => fs.statSync(file).isFile());
};

test("tracked TUI QA files and snapshots omit private infrastructure values", () => {
  const trackedForbidden = [
    ["TUI_QA", "BASE_IMAGE"].join("_"),
    ["TUI_QA", "BASE_FLAVOR"].join("_"),
    ["TUI_QA", "CA_CERT"].join("_"),
    ["NODE_EXTRA", "CA_CERTS"].join("_"),
    ...privateValues()
  ];
  for (const relative of trackedFiles()) {
    const content = fs.readFileSync(path.join(repositoryRoot, relative), "utf8");
    for (const value of trackedForbidden) assert.equal(content.includes(value), false, `${relative} contains a private infrastructure value`);
  }

  const snapshotForbidden = [
    ...privateValues(),
    ["file:", "//"].join(""),
    ["", "Users", ""].join("/"),
    ["", "home", ""].join("/"),
    ["", "tmp", ""].join("/"),
    ["", "var", "folders", ""].join("/")
  ];
  for (const file of snapshotFiles()) {
    const content = fs.readFileSync(file, "utf8");
    for (const value of snapshotForbidden) assert.equal(content.includes(value), false, `${path.relative(artifactRoot, file)} contains a private path or value`);
  }
});

import crypto from "node:crypto";
import fs from "node:fs";
import path from "node:path";
import process from "node:process";

import { generateMarkdownReport, readResults } from "./results.js";
import { rejectSymlinkEscape } from "./sandbox.js";

const repoRoot = process.env.AM_TUI_QA_REPO_ROOT;
const source = process.env.AM_TUI_QA_BASELINE_RESULTS;
if (!repoRoot || !source) throw new Error("repository root and baseline results path are required");
const results = readResults(source);
if (results.runClass !== "baseline" || !results.certifying) {
  throw new Error("only a certifying baseline result may be published");
}
if (!/^[A-Za-z0-9._-]{1,100}$/.test(results.runId)) throw new Error("baseline run ID is not a safe filename");

const publishNoReplace = (destination: string, contents: string): void => {
  const temporary = `${destination}.tmp-${process.pid}-${crypto.randomUUID()}`;
  let operationError: unknown;
  try {
    fs.writeFileSync(temporary, contents, { encoding: "utf8", flag: "wx", mode: 0o644 });
    try {
      fs.linkSync(temporary, destination);
    } catch (error) {
      if ((error as NodeJS.ErrnoException).code === "EEXIST") throw new Error(`refusing to replace published baseline: ${destination}`);
      throw error;
    }
  } catch (error) {
    operationError = error;
    throw error;
  } finally {
    try {
      if (fs.existsSync(temporary)) fs.rmSync(temporary);
    } catch (cleanupError) {
      if (operationError) throw new AggregateError([operationError, cleanupError], "baseline publication and temporary cleanup failed");
      throw cleanupError;
    }
  }
};

const destination = path.join(repoRoot, "docs", "qa", "tui");
rejectSymlinkEscape(destination, repoRoot);
fs.mkdirSync(destination, { recursive: true, mode: 0o755 });
const resultsPath = path.join(destination, `${results.runId}.results.json`);
const reportPath = path.join(destination, `${results.runId}.report.md`);
let resultsPublished = false;
try {
  publishNoReplace(resultsPath, JSON.stringify(results, null, 2) + "\n");
  resultsPublished = true;
  publishNoReplace(reportPath, generateMarkdownReport(results));
} catch (error) {
  if (resultsPublished) {
    try {
      fs.rmSync(resultsPath);
    } catch (cleanupError) {
      throw new AggregateError([error, cleanupError], "baseline publication and rollback failed");
    }
  }
  throw error;
}
process.stdout.write(`${resultsPath}\n${reportPath}\n`);

import path from "node:path";
import { defineConfig } from "@microsoft/tui-test";

import { readBoundedPositiveInt } from "./dist/options.js";

const artifactDir = process.env.AM_TUI_QA_ARTIFACT_DIR ?? ".local/agent-marketplace/qa/tui";
// The workbench owns the retry/FLAKY contract so it can record every attempt;
// the driver itself must not retry internally.
const retries = 0;
const timeout = readBoundedPositiveInt("AM_TUI_QA_TIMEOUT_MS", process.env.AM_TUI_QA_TIMEOUT_MS, 15_000, 120_000);
const columns = readBoundedPositiveInt("AM_TUI_QA_COLUMNS", process.env.AM_TUI_QA_COLUMNS, 80, 1_000);
const rows = readBoundedPositiveInt("AM_TUI_QA_ROWS", process.env.AM_TUI_QA_ROWS, 24, 1_000);
const suite = process.env.AM_TUI_QA_SUITE ?? "driver";
if (suite !== "driver" && suite !== "invariant") throw new Error("AM_TUI_QA_SUITE must be driver or invariant");
const testMatch = suite === "driver" ? "tests/pty-driver.spec.ts" : "tests/invariant.spec.ts";

export default defineConfig({
  retries,
  timeout,
  trace: true,
  traceFolder: path.join(artifactDir, "traces"),
  testMatch,
  workers: 1,
  use: {
    columns,
    rows,
    env: {
      PATH: process.env.PATH,
      TERM: "xterm-256color",
      LANG: "C.UTF-8",
      AM_TUI_QA_ARTIFACT_DIR: artifactDir
    },
    program: {
      file: "true"
    }
  }
});

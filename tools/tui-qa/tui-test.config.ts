import path from "node:path";
import process from "node:process";

import { defineConfig } from "@microsoft/tui-test";

const artifactRoot = process.env.AM_TUI_QA_ARTIFACT_DIR ?? ".local/agent-marketplace/qa/tui";

export default defineConfig({
  retries: 0,
  timeout: 60_000,
  trace: true,
  traceFolder: path.join(artifactRoot, "raw-traces"),
  testMatch: "tests/terminal-core.spec.ts",
  workers: 1,
  use: {
    columns: 80,
    rows: 24,
    env: { PATH: process.env.PATH, TERM: "xterm-256color", LANG: "C.UTF-8" },
    program: { file: "true" }
  }
});

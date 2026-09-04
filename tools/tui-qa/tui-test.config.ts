import { defineConfig } from "@microsoft/tui-test";

export default defineConfig({
  expect: { timeout: 5_000 },
  retries: 0,
  timeout: 60_000,
  testMatch: "tests/*.spec.ts",
  workers: 1,
  use: {
    columns: 80,
    rows: 24,
    program: { file: "true" }
  }
});

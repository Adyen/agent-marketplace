import fs from "node:fs";
import path from "node:path";

import { defineFixture } from "./helpers.js";

const output = process.env.AM_TUI_RECORDING_FIXTURE;
if (!output) throw new Error("AM_TUI_RECORDING_FIXTURE is required");

const lifecycle = defineFixture("recording-poc", []);
try {
  await lifecycle.setup();
  const fixture = lifecycle.current();
  const quote = (value: string): string => `'${value.replaceAll("'", `'"'"'`)}'`;
  const wrapper = path.join(path.dirname(output), "launch-recording-fixture");
  const environment = Object.entries(fixture.env)
    .map(([key, value]) => `  ${key}=${quote(value)}`)
    .join(" \\\n");

  fs.writeFileSync(
    wrapper,
    `#!/bin/sh\nexec env -i \\\n${environment} \\\n  ${quote(fixture.launcher)}\n`,
    { mode: 0o700 }
  );
  fs.writeFileSync(output, JSON.stringify({ root: fixture.root, wrapper }, null, 2));
} catch (error) {
  lifecycle.cleanup();
  throw error;
}

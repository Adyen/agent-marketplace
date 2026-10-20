import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { after, before, describe } from "node:test";

import { defineFixture, locate, step, waitForExit } from "./helpers.js";

// Read bytes and metadata from one descriptor so both describe the same file.
function snapshotFile(file: string): { bytes: Buffer; mtime: number; mode: number } {
  const fd = fs.openSync(file, "r");
  try {
    const stat = fs.fstatSync(fd);
    return { bytes: fs.readFileSync(fd), mtime: stat.mtimeMs, mode: stat.mode };
  } finally {
    fs.closeSync(fd);
  }
}

// One refusal journey through both launch routes, not another config-policy matrix.
for (const [route, args] of [["bare", []], ["explicit", ["tui"]]] as const) {
  describe(`${route} TTY startup`, () => {
    const journey = defineFixture(`unsupported-consumer-config-${route}`, [...args]);
    const unsupported = "apiVersion: agent-marketplace/v2\nharnesses: [factory]\nregistries: []\n";
    const baseline = new Map<string, { bytes: Buffer; mtime: number; mode: number }>();
    before(async () => {
      await journey.setup();
      const fixture = journey.current();
      fs.writeFileSync(path.join(fixture.home, ".agents", "config.yaml"), unsupported);
      for (const file of [
        path.join(fixture.home, ".agents", "config.yaml"),
        path.join(fixture.repo, ".agents", "config.yaml"),
        path.join(fixture.repo, ".agents", "lock.yaml"),
        path.join(fixture.repo, ".factory", "skills", "beta-skill", "SKILL.md")
      ]) {
        baseline.set(file, snapshotFile(file));
      }
    });
    after(journey.cleanup);

    // spec: consumer-config-format/consumers-do-not-offer-in-process-conversion
    // spec: tui-interactions/guarded-submission
    journey.test("unsupported consumer startup shows a format refusal and blocks new work", async (terminal) => {
      const fixture = journey.current();
      await step("startup refuses instead of offering conversion", () => {}, async () => {
        await locate(terminal, /! \d+ errors/).unique().expect();
        await locate(terminal, "Configuration migration required").expect({ not: true });
      });
      await step("open the path-specific format diagnostic", () => terminal.write("e"), async () => {
        await locate(terminal, "v4.api-version").expect();
        await locate(terminal, "apiVersion").expect();
        await locate(terminal, "config.yaml").expect();
      });
      await step("return from diagnostics", () => terminal.write("\x1b"), async () => {
        await locate(terminal, /! \d+ errors/).unique().expect();
      });
      for (const key of ["s", "u", "H"]) {
        await step(`refuse ${key} without a conversion action`, () => terminal.write(key), async () => {
          await locate(terminal, "configuration is invalid").expect();
          await locate(terminal, "Configuration migration required").expect({ not: true });
        });
      }
      await step("quit the refused workspace", () => terminal.write("q"), async () => {
        assert.equal((await waitForExit(terminal)).exitCode, 0);
      });
      for (const [file, prior] of baseline) {
        const after = snapshotFile(file);
        assert.deepEqual(after.bytes, prior.bytes, `refusal changed ${file}`);
        assert.equal(after.mtime, prior.mtime, `refusal rewrote ${file}`);
        assert.equal(after.mode, prior.mode, `refusal changed permissions of ${file}`);
      }
      assert.equal(fs.lstatSync(path.join(fixture.repo, ".factory", "skills", "beta-skill")).isSymbolicLink(), true);
      for (const file of [path.join(fixture.home, ".agents", "lock.yaml"), path.join(fixture.repo, ".agents", "config.local.yaml"), path.join(fixture.repo, ".agents", "lock.local.yaml")]) {
        assert.equal(fs.existsSync(file), false, `refusal created ${file}`);
      }
      assert.equal(fs.readdirSync(path.join(fixture.home, ".agents")).some((name) => name.startsWith("config.yaml.pre-v4.")), false);
    });
  });
}

import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";

import { expect, test } from "@microsoft/tui-test";

import { defineFixture, step, waitForExit } from "./helpers.js";

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
  test.describe(`${route} TTY startup`, () => {
    const journey = defineFixture(`unsupported-consumer-config-${route}`, [...args]);
    const unsupported = "apiVersion: agent-marketplace/v2\nharnesses: [factory]\nregistries: []\n";
    const before = new Map<string, { bytes: Buffer; mtime: number; mode: number }>();
    test.use(journey.options);
    test.beforeAll(async () => {
      await journey.setup();
      const fixture = journey.current();
      fs.writeFileSync(path.join(fixture.home, ".agents", "config.yaml"), unsupported);
      for (const file of [
        path.join(fixture.home, ".agents", "config.yaml"),
        path.join(fixture.repo, ".agents", "config.yaml"),
        path.join(fixture.repo, ".agents", "lock.yaml"),
        path.join(fixture.repo, ".factory", "skills", "beta-skill", "SKILL.md")
      ]) {
        before.set(file, snapshotFile(file));
      }
    });
    test.afterAll(journey.cleanup);

    // spec: consumer-config-format/consumers-do-not-offer-in-process-conversion
    // spec: tui-interactions/guarded-submission
    test("unsupported consumer startup shows a format refusal and blocks new work", async ({ terminal }) => {
      const fixture = journey.current();
      await step("startup refuses instead of offering conversion", () => {}, async () => {
        await expect(terminal.getByText(/! \d+ errors/g)).toBeVisible();
        await expect(terminal.getByText("Configuration migration required", { strict: false })).not.toBeVisible();
      });
      await step("open the path-specific format diagnostic", () => terminal.write("e"), async () => {
        await expect(terminal.getByText("v4.api-version", { strict: false })).toBeVisible();
        await expect(terminal.getByText("apiVersion", { strict: false })).toBeVisible();
        await expect(terminal.getByText("config.yaml", { strict: false })).toBeVisible();
      });
      await step("return from diagnostics", () => terminal.write("\x1b"), async () => {
        await expect(terminal.getByText(/! \d+ errors/g)).toBeVisible();
      });
      for (const key of ["s", "u", "H"]) {
        await step(`refuse ${key} without a conversion action`, () => terminal.write(key), async () => {
          await expect(terminal.getByText("configuration is invalid", { strict: false })).toBeVisible();
          await expect(terminal.getByText("Configuration migration required", { strict: false })).not.toBeVisible();
        });
      }
      await step("quit the refused workspace", () => terminal.write("q"), async () => {
        assert.equal((await waitForExit(terminal)).exitCode, 0);
      });
      for (const [file, prior] of before) {
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

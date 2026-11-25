import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { after, before } from "node:test";

import { defineFixture, locate, step, waitForExit } from "./helpers.js";
import { readRegularFile, runNonTTY } from "./process-helpers.js";

const observe = (files: string[]) => files.map((file) => {
  const stat = fs.lstatSync(file, { bigint: true, throwIfNoEntry: false });
  if (!stat) return { file, absent: true };
  let content: string[] | string | Buffer;
  if (stat.isDirectory()) content = fs.readdirSync(file).sort();
  else if (stat.isSymbolicLink()) content = fs.readlinkSync(file);
  else content = readRegularFile(file);
  return { file, content, mode: stat.mode, ino: stat.ino, nlink: stat.nlink, mtime: stat.mtimeNs, ctime: stat.ctimeNs };
});

// spec: session-interactions/unknown-harness-target-ids-refuse-configuration
// spec: consumer-config-format/unknown-harness-targets-gate-configuration-dependent-actions
for (const scope of ["user", "project"] as const) {
  for (const field of ["harnesses", "materialization"] as const) {
    const code = field === "harnesses" ? "v4.harness" : "v4.auxiliary";
    const journey = defineFixture(`unknown-harness-${scope}-${field}`, ["tui"], 80, 24);
    let config: string;
    let good: Buffer;
    let controlled: string[];
    let baseline: ReturnType<typeof observe>;
    before(async () => {
      await journey.setup();
      const fixture = journey.current();
      const project = path.join(fixture.repo, ".agents", "config.yaml");
      fs.writeFileSync(project, fixture.expectedRepoConfig.replace(
        "      - skill/beta-skill\n", "      - skill/beta-skill\n      - skill/alpha-skill\n"));
      config = scope === "user" ? path.join(fixture.home, ".agents", "config.yaml") : project;
      good = readRegularFile(config);
      const bad = field === "harnesses"
        ? good.toString("utf8").replace("harnesses:\n  - factory\n", "") + "harnesses: [factory, future-tool]\n"
        : good.toString("utf8") + "materialization: {targets: {factory: file, future-tool: file}}\n";
      fs.writeFileSync(config, bad);
      const skills = path.join(fixture.repo, ".factory", "skills");
      controlled = [
        ...[fixture.home, fixture.repo].flatMap((root) =>
          ["", "config.yaml", "config.local.yaml", "lock.yaml", "lock.local.yaml"].map((name) => path.join(root, ".agents", name))),
        skills, path.join(skills, "beta-skill"), path.join(skills, "beta-skill", "SKILL.md"), path.join(skills, "alpha-skill")
      ];
      baseline = observe(controlled);
      assert.equal(fs.lstatSync(path.join(skills, "alpha-skill"), { throwIfNoEntry: false }), undefined);
    });
    after(journey.cleanup);

    journey.test(`${scope} ${field} refuses an unknown target without dropping it or syncing the supported subset`, async (terminal) => {
      const fixture = journey.current();
      const unchanged = (): void => assert.deepEqual(observe(controlled), baseline, "refusal changed controlled files or left staging");
      await step("startup refuses the unknown target as a configuration error", () => {}, async () => {
        await locate(terminal, /! 1 errors:/).unique().expect({ timeout: 30_000 });
        await locate(terminal, "~ in sync").expect({ not: true });
      });
      await step("harness editing cannot rewrite the invalid configuration", () => terminal.write("H"), async () => {
        await locate(terminal, "configuration is invalid; press E to edit config").expect();
        await locate(terminal, "Target harnesses").expect({ not: true });
      });
      unchanged();
      await step("clear the harness refusal toast before testing sync", () => {}, async () => {
        await locate(terminal, "configuration is invalid; press E to edit config").expect({ not: true, timeout: 5_000 });
      });
      await step("sync is guarded rather than using Factory alone", () => terminal.write("s"), async () => {
        await locate(terminal, "configuration is invalid; press E to edit config").expect();
      });
      await step("errors identify the unknown harness setting", () => terminal.write("e"), async () => {
        await locate(terminal, "Errors (1)").unique().expect();
        const screen = (await terminal.text()).replace(/\s+/g, "");
        for (const expected of [`${code}:`, "future-tool", field === "harnesses" ? "harnesses[1]" : "materialization.targets"]) {
          assert.ok(screen.includes(expected), `the errors screen lost ${expected}:\n${await terminal.text()}`);
        }
      });
      unchanged();
      const refused = runNonTTY(fixture, ["sync"], 30_000);
      if (refused.error) throw refused.error;
      assert.equal(refused.status, 1, `${refused.stdout}\n${refused.stderr}`);
      assert.ok(refused.stderr.includes(`${code}:`), refused.stderr);
      assert.match(refused.stderr, /future-tool/);
      unchanged();

      await step("close errors", () => terminal.write("e"), async () => {
        await locate(terminal, "Errors (1)").expect({ not: true });
      });
      fs.writeFileSync(config, good);
      baseline = observe(controlled);
      await step("explicit repair and reload expose the pending supported resource", () => terminal.write("r"), async () => {
        await locate(terminal, "~ 1 out of sync").unique().expect({ timeout: 30_000 });
        await locate(terminal, /! \d+ errors/).expect({ not: true });
      });
      unchanged();
      await step("supported configuration permits harness editing again", () => terminal.write("H"), async () => {
        await locate(terminal, "Target harnesses").unique().expect();
        await locate(terminal, "[•] Factory Droid").unique().expect();
      });
      await step("cancel harness editing without writing", () => terminal.write("\x1b"), async () => {
        await locate(terminal, "Target harnesses").expect({ not: true });
      });
      unchanged();
      const beta = path.join(fixture.repo, ".factory", "skills", "beta-skill");
      const betaBefore = observe([beta, path.join(beta, "SKILL.md")]);
      await step("supported synchronization really installs the previously blocked resource", () => terminal.write("s"), async () => {
        await locate(terminal, "~ in sync").unique().expect({ timeout: 30_000 });
      });
      assert.deepEqual(readRegularFile(path.join(fixture.repo, ".factory", "skills", "alpha-skill", "SKILL.md")),
        Buffer.from(fixture.expectedAlphaSkillContent));
      assert.deepEqual(observe([beta, path.join(beta, "SKILL.md")]), betaBefore, "recovery changed the installed beta control");
      assert.deepEqual(readRegularFile(config), good, "recovery rewrote the repaired configuration");
      await step("quit", () => terminal.write("q"), async () => {
        assert.equal((await waitForExit(terminal)).exitCode, 0);
      });
    });
  }
}

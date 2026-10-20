import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { after, before } from "node:test";

import { defineFixture, locate, step, waitForExit } from "./helpers.js";
import { installFinding } from "./marketplace-stub.js";
import { runAM } from "./process-helpers.js";

// spec: tui-interactions/marketplace-findings-require-consent-before-install
const journey = defineFixture("marketplace-install-consent", ["tui"], 80, 24, { marketplaceDiscovery: "findings" });
before(journey.setup);
after(journey.cleanup);

journey.test("a Marketplace skill with findings installs only after consent", async (terminal) => {
  const fixture = journey.current();
  const repoConfig = path.join(fixture.repo, ".agents", "config.yaml");
  const repoLock = path.join(fixture.repo, ".agents", "lock.yaml");
  const userConfig = path.join(fixture.home, ".agents", "config.yaml");
  const userLock = path.join(fixture.home, ".agents", "lock.yaml");
  const betaSkill = path.join(fixture.repo, ".factory", "skills", "beta-skill", "SKILL.md");
  const installed = path.join(fixture.repo, ".factory", "skills", "org-review");

  await step("settled fixture baseline", () => {}, async () => {
    await locate(terminal, "beta-skill").expect();
    await locate(terminal, "~ in sync").unique().expect({ timeout: 30_000 });
  });
  const unchangedByDecline = [repoConfig, repoLock, userConfig, userLock, betaSkill].map((file) => ({ file, bytes: fs.readFileSync(file) }));

  await step("search the Marketplace", () => terminal.write("/org-review"), async () => {
    await locate(terminal, "market/tools/org-review").expect({ timeout: 10_000 });
  });
  await step("apply the search", () => terminal.write("\r"), async () => {
    await locate(terminal, "1 match · esc clear").expect();
  });
  const requestInstall = async (attempt: string): Promise<void> => {
    await step(`${attempt}: open the install scopes`, () => terminal.write(" "), async () => {
      await locate(terminal, "toggle project").unique().expect();
    });
    await step(`${attempt}: the verified plan asks for consent and shows its finding`, () => terminal.write("p"), async () => {
      await locate(terminal, "Verified Marketplace install").unique().expect({ timeout: 30_000 });
      await locate(terminal, "findings (1):").unique().expect();
      await locate(terminal, installFinding.message).unique().expect();
      await locate(terminal, "y install · n/esc cancel").expect();
    });
  };

  await requestInstall("first request");
  await step("decline the install", () => terminal.write("n"), async () => {
    await locate(terminal, "Verified Marketplace install").expect({ not: true });
    await locate(terminal, /Status:\s+available via Marketplace/).unique().expect();
  });
  for (const { file, bytes } of unchangedByDecline) {
    assert.deepEqual(fs.readFileSync(file), bytes, `declining changed ${file}`);
  }
  assert.equal(fs.lstatSync(installed, { throwIfNoEntry: false }), undefined, "declining installed the skill");

  await requestInstall("second request");
  await step("accept the install", () => terminal.write("y"), async () => {
    await locate(terminal, /Enabled\s+\(1\)/).unique().expect({ timeout: 30_000 });
    await locate(terminal, "~ in sync").unique().expect({ timeout: 30_000 });
  });
  assert.match(fs.readFileSync(repoConfig, "utf8"), /\n      - tools\/skill\/org-review\n/);
  assert.ok(fs.readFileSync(repoLock, "utf8").includes(`\n        name: org-review\n        revision: ${fixture.marketplaceContentDigest}\n`));
  assert.equal(fs.readFileSync(path.join(installed, "SKILL.md"), "utf8"), fixture.expectedOrgSkillContent);

  await step("quit", () => terminal.write("q"), async () => {
    assert.equal((await waitForExit(terminal)).exitCode, 0);
  });

  // runAM keeps the event loop free so the in-process Marketplace stub can answer.
  const check = await runAM(fixture, ["sync", "--check"]);
  assert.equal(check.status, 0, `${check.stdout}\n${check.stderr}`);
});

import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { after, before } from "node:test";

import { defineFixture, locate, lockedGitCommit, step, waitForExit } from "./helpers.js";
import { runAM } from "./process-helpers.js";

// spec: tui-interactions/discovered-marketplace-skill-installs-at-the-chosen-scope
// spec: tui-interactions/marketplace-install-keeps-locked-sources-at-their-locked-revision
const journey = defineFixture("marketplace-discovery-install", ["tui"], 80, 24, { marketplaceDiscovery: "plain" });
before(journey.setup);
after(journey.cleanup);

journey.test("installs a discovered Marketplace skill while locked Git sources stay at their locked revision", async (terminal) => {
  const fixture = journey.current();
  const repoConfig = path.join(fixture.repo, ".agents", "config.yaml");
  const repoLock = path.join(fixture.repo, ".agents", "lock.yaml");
  const skills = path.join(fixture.repo, ".factory", "skills");

  await step("settled fixture baseline", () => {}, async () => {
    await locate(terminal, "beta-skill").expect();
    await locate(terminal, "~ in sync").unique().expect({ timeout: 30_000 });
  });
  const commitBefore = lockedGitCommit(fs.readFileSync(repoLock, "utf8"));
  fixture.pushRegistryChange(
    "skills/beta-skill/SKILL.md",
    "---\nname: beta-skill\ndescription: Unlocked upstream revision\n---\n\n# Beta Skill\n\nNot pinned by the lock.\n"
  );

  await step("open search", () => terminal.write("/"), async () => {
    await locate(terminal, /\/\s{20,}/).unique().expect();
  });
  await step("the Marketplace answers with the uninstalled skill", () => terminal.write("org-review"), async () => {
    await locate(terminal, "market/tools/org-review").expect({ timeout: 10_000 });
    await locate(terminal, /Status:\s+available via Marketplace/).unique().expect();
  });
  await step("apply the search", () => terminal.write("\r"), async () => {
    await locate(terminal, "1 match · esc clear").expect();
  });
  await step("open the install scopes", () => terminal.write(" "), async () => {
    await locate(terminal, '"org-review"').expect();
    await locate(terminal, "toggle project").unique().expect();
  });
  await step("install at project scope without a consent prompt", () => terminal.write("p"), async () => {
    await locate(terminal, /Enabled\s+\(1\)/).unique().expect({ timeout: 30_000 });
    await locate(terminal, "~ in sync").unique().expect({ timeout: 30_000 });
    await locate(terminal, "Verified Marketplace install").expect({ not: true });
  });

  const config = fs.readFileSync(repoConfig, "utf8");
  assert.ok(config.includes(`\n    marketplace: ${fixture.marketplaceURL}\n`), `repository config does not declare the Marketplace:\n${config}`);
  assert.match(config, /\n      - tools\/skill\/org-review\n/);
  const lock = fs.readFileSync(repoLock, "utf8");
  assert.ok(lock.includes(`\n        name: org-review\n        revision: ${fixture.marketplaceContentDigest}\n`), `repository lock does not pin org-review:\n${lock}`);
  assert.equal(lockedGitCommit(lock), commitBefore, "the install moved the Git source's locked commit");
  assert.equal(fs.readFileSync(path.join(skills, "org-review", "SKILL.md"), "utf8"), fixture.expectedOrgSkillContent);
  assert.equal(fs.readFileSync(path.join(skills, "beta-skill", "SKILL.md"), "utf8"), fixture.expectedBetaSkillContent, "beta-skill left its locked revision");

  await step("quit", () => terminal.write("q"), async () => {
    assert.equal((await waitForExit(terminal)).exitCode, 0);
  });

  // runAM keeps the event loop free so the in-process Marketplace stub can answer.
  const check = await runAM(fixture, ["sync", "--check"]);
  assert.equal(check.status, 0, `${check.stdout}\n${check.stderr}`);
});

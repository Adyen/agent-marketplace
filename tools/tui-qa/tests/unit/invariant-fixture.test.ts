import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import fs from "node:fs";
import path from "node:path";
import { describe, it } from "node:test";

import { seedInvariantFixture } from "../../src/invariant-fixture.js";
import { environmentForTest, provision } from "../../src/sandbox.js";

describe("seedInvariantFixture", () => {
  it("creates a normal local-registry workspace through public configuration", (t) => {
    const sandbox = provision("unit", "invariant-fixture", 1);
    t.after(() => fs.rmSync(sandbox.root, { recursive: true, force: true }));
    const env = environmentForTest(sandbox);
    const fixture = seedInvariantFixture(sandbox, env);

    assert.equal(fixture.id, "local-bare-git-registry-v1");
    assert.equal(fs.readFileSync(path.join(sandbox.registries, "fixture-source", "skills", "alpha-skill", "SKILL.md"), "utf8").includes("name: alpha-skill"), true);
    assert.equal(fs.existsSync(path.join(sandbox.registries, "fixture.git", "HEAD")), true);
    assert.equal(fs.readFileSync(path.join(sandbox.home, ".agents", "config.yaml"), "utf8").includes("harnesses:\n  - factory"), true);
    assert.equal(fs.readFileSync(path.join(sandbox.repo, ".agents", "config.yaml"), "utf8").includes("url: file://"), true);
    assert.equal(execFileSync("git", ["rev-parse", "--is-inside-work-tree"], { cwd: sandbox.repo, env, encoding: "utf8" }).trim(), "true");
  });
});

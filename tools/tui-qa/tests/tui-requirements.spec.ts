import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";

import { expect, test } from "@microsoft/tui-test";

import { defineFixture, step, waitForExit } from "./helpers.js";

const journey = defineFixture("tui-requirements", ["tui"], 80, 24, { directorySource: true });
test.use(journey.options);
test.beforeAll(journey.setup);
test.afterAll(journey.cleanup);

// Seeds a directory source whose parent-skill declares requires: child-skill,
// plus a v4 repository config that selects only parent-skill with expansion
// enabled. Runs after the generic fixture setup so the TUI launches with the
// prerequisite topology already resolved.
test.beforeAll(async () => {
  const fixture = journey.current();
  const source = fixture.registryURL;
  const writeSkill = (name: string, requires: string) =>
    fs.writeFileSync(
      path.join(source, "skills", name, "SKILL.md"),
      `---\nname: ${name}\ndescription: Prerequisite fixture ${name}\n${requires}---\n\n# ${name}\n`,
      { encoding: "utf8", mode: 0o644 }
    );
  for (const name of ["parent-skill", "child-skill"]) {
    fs.mkdirSync(path.join(source, "skills", name), { recursive: true, mode: 0o700 });
  }
  writeSkill("parent-skill", "metadata:\n  requires:\n    - skill:child-skill\n");
  writeSkill("child-skill", "");
  const repoConfig = `apiVersion: agent-marketplace/v4\nharnesses: [factory]\nrequires: {expand: true}\nsources:\n  reqsrc:\n    directory: ${source}\nresources:\n  reqsrc:\n    include:\n      - skill/parent-skill\n`;
  fs.mkdirSync(path.join(fixture.repo, ".agents"), { recursive: true, mode: 0o700 });
  fs.writeFileSync(path.join(fixture.repo, ".agents", "config.yaml"), repoConfig, { encoding: "utf8", mode: 0o600 });
  // The generic fixture seeds the same directory under the user layer's `local`
  // alias; drop it so child-skill resolves only through the repo source and the
  // project-scope exclusion below governs it.
  fs.writeFileSync(path.join(fixture.home, ".agents", "config.yaml"), "apiVersion: agent-marketplace/v4\nharnesses: [factory]\n", { encoding: "utf8", mode: 0o600 });
});

test("prerequisite sections, markers, and toggle feedback", async ({ terminal }) => {
  const fixture = journey.current();
  const repoConfig = path.join(fixture.repo, ".agents", "config.yaml");

  await step("expansion-selected child carries the req marker", () => {}, async () => {
    await expect(terminal.getByText("parent-skill", { strict: false })).toBeVisible();
    await expect(terminal.getByText(/child-skill\s+req/g, { strict: false })).toBeVisible();
  });

  await step("filter for parent", () => terminal.write("/parent-skill\r"), async () => {
    await expect(terminal.getByText(/Name:\s+parent-skill/g)).toBeVisible();
  });

  await step("detail shows the implied prerequisite", () => {}, async () => {
    await expect(terminal.getByText("Requires:", { strict: false })).toBeVisible();
    await expect(terminal.getByText(/implied\s+skill\s+"child-skill"/g)).toBeVisible();
  });

  await step("clear filter", () => terminal.write("\x1b"), async () => {
    await expect(terminal.getByText("beta-skill", { strict: false })).toBeVisible();
  });

  await step("open parent project toggle", () => terminal.write("/parent-skill\r "), async () => {
    await expect(terminal.getByText("toggle project")).toBeVisible();
  });

  await step("toggle parent off", () => terminal.write("p"), async () => {
    await expect(terminal.getByText(/Status:\s+(available|excluded)/g)).toBeVisible({ timeout: 15_000 });
  });

  await step("open parent project toggle again", () => terminal.write(" "), async () => {
    await expect(terminal.getByText("toggle project")).toBeVisible();
  });

  await step("toggle parent on reports the also-selected prerequisite", () => terminal.write("p"), async () => {
    await expect(
      terminal.getByText(/also selected skill "child-skill" \(required by skill "parent-skill"\)/g)
    ).toBeVisible({ timeout: 15_000 });
  });

  await step("clear filter", () => terminal.write("\x1b"), async () => {
    await expect(terminal.getByText("esc clear", { strict: false })).not.toBeVisible();
  });

  await step("filter to the implied child", () => terminal.write("/child-skill\r"), async () => {
    await expect(terminal.getByText(/Name:\s+child-skill/g)).toBeVisible();
  });

  await step("open child project toggle", () => terminal.write(" "), async () => {
    await expect(terminal.getByText("toggle project")).toBeVisible();
  });

  await step("toggle child off excludes it and warns the dependent is unmet", () => terminal.write("p"), async () => {
    await (async () => {
      const deadline = Date.now() + 15_000;
      while (Date.now() < deadline) {
        if (/exclude:[^]*child-skill/.test(fs.readFileSync(repoConfig, "utf8"))) return;
        await new Promise((resolve) => setTimeout(resolve, 250));
      }
      assert.match(fs.readFileSync(repoConfig, "utf8"), /exclude:[^]*child-skill/);
    })();
    // The footer counts the resulting unmet prerequisite; its diagnostics
    // pane shows the warning text durably.
    await expect(terminal.getByText(/⚠ \d+ warnings/g)).toBeVisible({ timeout: 30_000 });
    await terminal.write("e");
    await expect(
      terminal.getByText(/skill "parent-skill" requires skill "child-skill", but it is not selected/g)
    ).toBeVisible();
    await terminal.write("\x1b");
    await expect(
      terminal.getByText(/skill "parent-skill" requires skill "child-skill", but it is not selected/g)
    ).not.toBeVisible();
    await expect(terminal.getByText(/child-skill\s+req/g, { strict: false })).not.toBeVisible();
  });

  await step("refilter to the requiring parent", async () => {
    await terminal.write("/");
    await terminal.write("\x15");
    await terminal.write("parent-skill\r");
  }, async () => {
    await expect(terminal.getByText(/parent-skill\s+⚠/g, { strict: false })).toBeVisible();
    await expect(terminal.getByText(/excluded\s+skill\s+"child-skill"/g)).toBeVisible();
  });

  await step("quit", async () => {
    await terminal.write("q");
    await expect(terminal.getByText(/Sync\+quit\?/g)).toBeVisible();
    await terminal.write("n");
  }, async () => {
    assert.equal((await waitForExit(terminal)).exitCode, 0);
  });
});

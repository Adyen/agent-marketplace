import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { after, before } from "node:test";

import { defineFixture, locate, step, waitForExit } from "./helpers.js";
import { write } from "./process-helpers.js";

// spec: tui-navigation/name-match-outranks-metadata-match
// spec: tui-navigation/metadata-only-match
// spec: tui-navigation/kind-and-source-searches-work-across-groupings
const journey = defineFixture("search-ranking", ["tui"], 80, 24, { directorySource: true });
before(async () => {
  await journey.setup();
  const source = journey.current().registryURL;
  for (const [name, description] of [
    ["aaa-guide", "Orbit examples for the fixture"],
    ["orbit", "Exact name fixture"],
    ["z-orbit", "Weaker name fixture"]
  ]) {
    write(path.join(source, "skills", name, "SKILL.md"), `---\nname: ${name}\ndescription: ${description}\n---\n\n# ${name}\n`);
  }
  write(path.join(source, "commands", "verify.md"), "---\nname: verify\ndescription: Run fixture checks\n---\n\nVerify the fixture.\n");
});
after(journey.cleanup);

journey.test("name matches rank above metadata, and kind-only searches work across groupings", async (terminal) => {
  const fixture = journey.current();
  const watched = [
    path.join(fixture.home, ".agents", "config.yaml"),
    path.join(fixture.home, ".agents", "lock.yaml"),
    path.join(fixture.repo, ".agents", "config.yaml"),
    path.join(fixture.repo, ".agents", "lock.yaml"),
    path.join(fixture.repo, ".agents", "config.local.yaml"),
    path.join(fixture.repo, ".agents", "lock.local.yaml")
  ];
  const snapshot = watched.map((file) => ({ file, bytes: fs.existsSync(file) ? fs.readFileSync(file) : undefined }));
  const names = ["aaa-guide", "alpha-skill", "beta-skill", "orbit", "verify", "z-orbit"];
  const row = (name: string) => locate(terminal, `source/${name}`).unique();
  const rowY = async (name: string): Promise<number> => (await row(name).location()).start.row;
  const allRows = async (): Promise<void> => {
    for (const name of names) await row(name).expect();
  };
  const noHighlight = async (name: string): Promise<void> => {
    await row(name).expect();
    await row(name).and(terminal.getByStyle({ foreground: "14" })).expect({ not: true });
  };
  const search = async (query: string, count: number, assertion: () => Promise<void>): Promise<void> => {
    await step(`type ${query}`, () => terminal.write(`/${query}`), async () => {
      await locate(terminal, `/ ${query}`).unique().expect();
      await locate(terminal, /enter apply.*esc cancel/).unique().expect();
    });
    await step(`apply ${query}`, () => terminal.write("\r"), async () => {
      await locate(terminal, `/ ${query}  ${count} match${count === 1 ? "" : "es"}`).unique().expect();
      await assertion();
    });
  };
  const clear = async (): Promise<void> => {
    await step("clear the search restores alphabetical rows", () => terminal.write("\x1b"), async () => {
      await locate(terminal, "esc clear").expect({ not: true });
      await allRows();
      assert.ok((await rowY("aaa-guide")) < (await rowY("orbit")), "clearing the query did not restore alphabetical order");
      await noHighlight("z-orbit");
    });
  };

  await step("settled mixed-kind directory catalog", () => {}, async () => {
    await locate(terminal, "~ in sync").unique().expect({ timeout: 30_000 });
    await locate(terminal, /Available\s+\(6\)/).unique().expect();
    await allRows();
  });

  for (const grouping of ["Status", "Source", "Kind"]) {
    if (grouping !== "Status") {
      await step(`open View settings for ${grouping}`, () => terminal.write("v"), async () => {
        await locate(terminal, "View settings").unique().expect();
      });
      await step(`preview ${grouping}`, () => terminal.write("l"), async () => {
        await locate(terminal, new RegExp(`Group by:.*\\[${grouping}\\]`)).unique().expect();
      });
      await step(`accept ${grouping}`, () => terminal.write("\r"), async () => {
        await locate(terminal, "View settings").expect({ not: true });
        if (grouping === "Source") {
          await locate(terminal, /source\s+\(6\)/).unique().expect();
        } else {
          await locate(terminal, /Skills\s+\(5\)/).unique().expect();
          await locate(terminal, /Commands\s+\(1\)/).unique().expect();
        }
        await allRows();
      });
    }

    await step(`${grouping}: select the first section`, () => terminal.write("g"), async () => {
      await locate(terminal, "Select a resource to see details").unique().expect();
    });
    await step(`${grouping}: select the alphabetically first metadata match`, () => terminal.write("j"), async () => {
      await locate(terminal, /Name:\s+aaa-guide/).unique().expect();
    });
    await search("orbit", 3, async () => {
      await locate(terminal, /Name:\s+orbit\b/).unique().expect();
      await row("orbit").expect();
      await row("z-orbit").expect();
      await row("aaa-guide").expect();
      assert.ok((await rowY("orbit")) < (await rowY("z-orbit")), `${grouping}: the exact name must outrank the weaker name`);
      assert.ok((await rowY("z-orbit")) < (await rowY("aaa-guide")), `${grouping}: name matches must outrank metadata`);
      // The unselected name match proves styling is observable; the unselected
      // metadata match must have no highlighted cells, not just no styled span.
      await row("z-orbit").and(terminal.getByStyle({ foreground: "14", bold: true, underlineStyle: "single" })).expect();
      await noHighlight("aaa-guide");
      await row("alpha-skill").expect({ not: true });
      await row("beta-skill").expect({ not: true });
      await row("verify").expect({ not: true });
    });
    await clear();

    await search("command", 1, async () => {
      await row("verify").expect();
      await locate(terminal, /Name:\s+verify\b/).unique().expect();
      await locate(terminal, /Kind:\s+command\b/).unique().expect();
      for (const name of names.filter((name) => name !== "verify")) await row(name).expect({ not: true });
      if (grouping === "Kind") {
        await locate(terminal, /Commands\s+\(1\)/).unique().expect();
        await locate(terminal, /Skills\s+\(/).expect({ not: true });
      }
    });
    await step(`${grouping}: inspect the unselected kind-only match`, () => terminal.write("k"), async () => {
      await locate(terminal, "Select a resource to see details").unique().expect();
      await noHighlight("verify");
    });
    await clear();

    await search("source", 6, async () => {
      await allRows();
      await locate(terminal, /Name:\s+aaa-guide/).unique().expect();
      await noHighlight("verify");
    });
    await clear();

    await search("zzzz", 0, async () => {
      await locate(terminal, "No resources found").unique().expect();
      await locate(terminal, /Name:/).expect({ not: true });
      await locate(terminal, /(Available|Skills|Commands|source)\s+\(\d+\)/).expect({ not: true });
      for (const name of names) await row(name).expect({ not: true });
    });
    await clear();
  }

  await step("quit", () => terminal.write("q"), async () => {
    assert.equal((await waitForExit(terminal)).exitCode, 0);
  });
  for (const { file, bytes } of snapshot) {
    if (bytes === undefined) assert.equal(fs.existsSync(file), false, `search created ${file}`);
    else {
      assert.equal(fs.existsSync(file), true, `search deleted ${file}`);
      assert.deepEqual(fs.readFileSync(file), bytes, `search changed ${file}`);
    }
  }
});

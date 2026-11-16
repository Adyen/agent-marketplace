import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { after, before } from "node:test";

import { defineFixture, locate, step } from "./helpers.js";

// spec: tui-navigation/every-kind-query-discovers-all-marketplace-matches
// spec: tui-navigation/other-kinds-retain-additive-text-matching
// spec: tui-navigation/failed-kind-discovery-preserves-usable-local-results
const journey = defineFixture("marketplace-kind-search", ["tui"], 80, 24, { marketplaceDiscovery: "mixed-denial" });
before(journey.setup);
after(journey.cleanup);

journey.test("kind queries discover opaque Marketplace resources and clear back to browse", async (terminal) => {
  const fixture = journey.current();
  const watched = [
    path.join(fixture.home, ".agents", "config.yaml"),
    path.join(fixture.repo, ".agents", "config.yaml"),
    path.join(fixture.repo, ".agents", "lock.yaml"),
    path.join(fixture.repo, ".factory", "skills", "beta-skill", "SKILL.md")
  ];
  const before = watched.map((file) => fs.readFileSync(file));
  const browseRows = async (): Promise<void> => {
    await locate(terminal, "alpha-skill").expect();
    await locate(terminal, "beta-skill").expect();
  };
  const openSearch = async (): Promise<void> => {
    await step("open search", () => terminal.write("/"), async () => {
      await locate(terminal, /\/\s{20,}/).unique().expect();
    });
  };
  const clearSearch = async (): Promise<void> => {
    await step("clear search", () => terminal.write("\x1b"), async () => {
      await locate(terminal, "esc clear").expect({ not: true });
      await browseRows();
    });
  };

  await step("settled Marketplace browse", () => {}, async () => {
    await locate(terminal, /! 1 errors: marketplace URL/).unique().expect({ timeout: 30_000 });
    await browseRows();
  });
  await step("show the denied endpoint diagnostic", () => terminal.write("e"), async () => {
    await locate(terminal, "Marketplace discovery (1)").unique().expect();
    await locate(
      terminal,
      "denied-market: marketplace URL(s) blocked by allowed_marketplaces policy"
    ).unique().expect();
  });
  await step("return to the resource list", () => terminal.write("\x1b"), browseRows);

  await openSearch();
  await step("discover opaque hosted MCP servers", () => terminal.write("mcp"), async () => {
    await locate(terminal, "market/tools/atlas").unique().expect({ timeout: 10_000 });
    await locate(terminal, "market/tools/beacon").unique().expect();
    await locate(terminal, "market/tools/atlas")
      .and(terminal.getByStyle({ foreground: "14", bold: true, underlineStyle: "single" }))
      .expect({ not: true });
  });
  await step("apply the MCP search", () => terminal.write("\r"), async () => {
    await locate(terminal, /2 matches.*esc clear/).unique().expect();
    await locate(terminal, "market/tools/atlas").unique().expect();
    await locate(terminal, "market/tools/beacon").unique().expect();
  });
  await clearSearch();

  await openSearch();
  await step("discover opaque hosted commands", () => terminal.write("command"), async () => {
    await locate(terminal, "market/tools/archivist").unique().expect({ timeout: 10_000 });
    await locate(terminal, "market/tools/sentinel").unique().expect();
  });
  await step("apply the command search", () => terminal.write("\r"), async () => {
    await locate(terminal, /2 matches.*esc clear/).unique().expect();
    await locate(terminal, "market/tools/archivist").unique().expect();
    await locate(terminal, "market/tools/sentinel").unique().expect();
  });
  await clearSearch();

  for (let i = 0; i < watched.length; i++) {
    assert.deepEqual(fs.readFileSync(watched[i]), before[i], `kind search changed ${watched[i]}`);
  }
});

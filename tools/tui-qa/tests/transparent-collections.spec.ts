import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { after, before } from "node:test";

import { defineFixture, expectSearchOpen, findTextRun, locate, step, waitForExit } from "./helpers.js";

// spec: tui-navigation/expanding-a-collection-shows-its-mcp-servers-and-skills
// spec: tui-interactions/installing-a-single-member-from-an-available-collection
const journey = defineFixture("transparent-collections", ["tui"], 80, 24, { transparentCollection: true });
before(journey.setup);
after(journey.cleanup);

journey.test("collection members expand under Kind and install as independent selections", async (terminal) => {
  const fixture = journey.current();
  const repoConfig = path.join(fixture.repo, ".agents", "config.yaml");
  const collectionDecoy = path.join(fixture.repo, "collections", "suite", "COLLECTION.yaml");

  await step("settled fixture lists the collection and its resources", () => {}, async () => {
    await locate(terminal, "fixture/suite").unique().expect();
    await locate(terminal, "fixture/alpha-skill").first().expect();
    await locate(terminal, "fixture/genai-psx").first().expect();
    await locate(terminal, "~ in sync").unique().expect({ timeout: 30_000 });
  });
  await step("open collection search", () => terminal.write("/"), async () => {
    await expectSearchOpen(terminal);
  });
  await step("filter to the collection", () => terminal.write("suite"), async () => {
    await locate(terminal, "/ suite").unique().expect();
  });
  await step("apply the collection filter", () => terminal.write("\r"), async () => {
    await locate(terminal, /Name:\s+suite/).unique().expect();
  });
  await step("open the selected collection's source browser", () => terminal.write("\r"), async () => {
    await locate(terminal, /COLLECTION\.yaml/).first().expect();
    await locate(terminal, /resources:/).unique().expect();
    await locate(terminal, /CONSUMER-COLLECTION-DECOY/).expect({ not: true });
    await locate(terminal, /Contains \(2\):/).unique().expect();
    await locate(terminal, /skill:\s+alpha-skill/).unique().expect();
    await locate(terminal, /mcpserver:\s+genai-psx/).unique().expect();
  });
  await step("return to the selected collection", () => terminal.write("\x1b"), async () => {
    await locate(terminal, /Name:\s+suite/).unique().expect();
    await locate(terminal, /Contains \(2\):/).unique().expect();
    await locate(terminal, "fixture/suite").unique().expect();
  });
  await step("open View settings", () => terminal.write("v"), async () => {
    await locate(terminal, /▸ Group by:\s+\[Status\]/).unique().expect();
  });
  await step("move to Source grouping", () => terminal.write("l"), async () => {
    await locate(terminal, /▸ Group by:\s+Status\s+\[Source\]/).unique().expect();
  });
  await step("move to Kind grouping", () => terminal.write("l"), async () => {
    await locate(terminal, /▸ Group by:.*\[Kind\]/).unique().expect();
  });
  await step("apply Kind grouping", () => terminal.write("\r"), async () => {
    await locate(terminal, /▸ Group by:/).expect({ not: true });
    await locate(terminal, /Collections/).unique().expect();
    await locate(terminal, /Name:\s+suite/).unique().expect();
    await locate(terminal, "fixture/alpha-skill").unique().expect();
    await locate(terminal, "fixture/genai-psx").unique().expect();
  });
  const parent = await findTextRun(terminal, /▼ collection/);
  const skill = await findTextRun(terminal, /skill/);
  const mcp = await findTextRun(terminal, /mcp fixture\/genai-psx/);
  assert.ok(skill.x > parent.x && mcp.x > parent.x, "collection members must be indented under their parent");
  await locate(terminal, /Contains \(2\):/).unique().expect();
  await locate(terminal, /mcpserver:\s+genai-psx/).unique().expect();
  await locate(terminal, /skill:\s+alpha-skill/).unique().expect();

  await step("collapse the collection", () => terminal.write("h"), async () => {
    await locate(terminal, "fixture/alpha-skill").expect({ not: true });
    await locate(terminal, "fixture/genai-psx").expect({ not: true });
  });
  await step("expand the collection", () => terminal.write("l"), async () => {
    await locate(terminal, "fixture/alpha-skill").unique().expect();
    await locate(terminal, "fixture/genai-psx").unique().expect();
  });
  await step("select the skill member", () => terminal.write("j"), async () => {
    await locate(terminal, /Name:\s+alpha-skill/).unique().expect();
  });
  await step("select the MCP member", () => terminal.write("j"), async () => {
    await locate(terminal, /Name:\s+genai-psx/).unique().expect();
  });
  await step("open the member toggle", () => terminal.write(" "), async () => {
    await locate(terminal, /"genai-psx"/).unique().expect();
    await locate(terminal, "toggle project").unique().expect();
  });
  await step("select only the MCP member at project scope", () => terminal.write("p"), async () => {
    await locate(terminal, /Name:\s+genai-psx/).unique().expect();
    await locate(terminal, "~ 1 out of sync").unique().expect({ timeout: 30_000 });
  });
  const config = fs.readFileSync(repoConfig, "utf8");
  assert.match(config, /- mcp\/genai-psx/);
  assert.match(config, /- skill\/beta-skill/);
  assert.doesNotMatch(config, /- collection\/suite|- skill\/alpha-skill/);

  await step("sync installs only the selected member", () => terminal.write("s"), async () => {
    await locate(terminal, "~ in sync").unique().expect({ timeout: 30_000 });
  });
  const mcpConfig = JSON.parse(fs.readFileSync(path.join(fixture.repo, ".factory", "mcp.json"), "utf8"));
  assert.deepEqual(Object.keys(mcpConfig.mcpServers), ["genai-psx"]);
  assert.equal(fs.existsSync(path.join(fixture.repo, ".factory", "skills", "alpha-skill")), false, "the unselected skill sibling was materialized");
  assert.equal(fs.readFileSync(collectionDecoy, "utf8"), "name: consumer-decoy\nresources:\n  - name: CONSUMER-COLLECTION-DECOY\n");

  await step("quit", () => terminal.write("q"), async () => {
    assert.equal((await waitForExit(terminal)).exitCode, 0);
  });
});

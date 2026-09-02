import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { describe, it } from "node:test";

import { type CodeInventory, validateCodeInventory } from "../../src/code-inventory.js";

const testDirectory = path.dirname(fileURLToPath(import.meta.url));
const qaRoot = [
  path.resolve(testDirectory, "../.."),
  path.resolve(testDirectory, "../../..")
].find((candidate) => fs.existsSync(path.join(candidate, "cmd", "tui-code-inventory")));
if (!qaRoot) throw new Error("TUI QA workbench root not found");

const exportInventory = (): string => execFileSync(
  "go",
  ["run", "./cmd/tui-code-inventory"],
  { cwd: qaRoot, encoding: "utf8", maxBuffer: 1024 * 1024 }
);

describe("code-owned TUI inventory", () => {
  it("exports deterministic schema-valid JSON", () => {
    const first = exportInventory();
    const second = exportInventory();
    assert.equal(second, first);

    const inventory: unknown = JSON.parse(first);
    assert.deepEqual(validateCodeInventory(inventory), []);
    assert.ok(validateCodeInventory({
      ...(inventory as Record<string, unknown>),
      generatedAt: "2026-09-01T00:00:00Z"
    }).length > 0);
  });

  it("rejects duplicate nested IDs even when the records differ", () => {
    const inventory = JSON.parse(exportInventory()) as CodeInventory;
    const duplicateAction = {
      ...inventory.actions[0],
      helpDescription: `${inventory.actions[0].helpDescription} changed`
    };
    const duplicateTarget = {
      ...inventory.targets[0],
      label: `${inventory.targets[0].label} changed`
    };
    assert.match(validateCodeInventory({ ...inventory, actions: [...inventory.actions, duplicateAction] }).join("; "), /duplicate action ID/);
    assert.match(validateCodeInventory({ ...inventory, targets: [...inventory.targets, duplicateTarget] }).join("; "), /duplicate target ID/);
  });

  it("rejects invalid action prefixes", () => {
    const inventory = JSON.parse(exportInventory()) as CodeInventory;
    const unknown = { ...inventory.actions[0], prefix: "missing-action" };
    const self = { ...inventory.actions[0], prefix: inventory.actions[0].id };
    const nested = { ...inventory.actions[0], prefix: "fold-toggle" };
    assert.match(validateCodeInventory({ ...inventory, actions: [unknown, ...inventory.actions.slice(1)] }).join("; "), /unknown prefix/);
    assert.match(validateCodeInventory({ ...inventory, actions: [self, ...inventory.actions.slice(1)] }).join("; "), /cannot prefix itself/);
    assert.match(validateCodeInventory({ ...inventory, actions: [nested, ...inventory.actions.slice(1)] }).join("; "), /nested prefix/);
  });
});

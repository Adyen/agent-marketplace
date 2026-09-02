import { compareStrings } from "./order.js";
import { validateAgainstSchema } from "./schema.js";

export interface CodeInventoryAction {
  id: string;
  prefix?: string;
  keys: string[];
  helpKey: string;
  helpDescription: string;
}

export interface CodeInventoryTarget {
  id: string;
  label: string;
}

export interface CodeInventory {
  schemaVersion: 1;
  screens: string[];
  actions: CodeInventoryAction[];
  states: string[];
  targets: CodeInventoryTarget[];
}

const duplicateIDs = (ids: string[]): string[] => {
  const seen = new Set<string>();
  const duplicates = new Set<string>();
  for (const id of ids) {
    if (seen.has(id)) duplicates.add(id);
    seen.add(id);
  }
  return [...duplicates].sort(compareStrings);
};

export const validateCodeInventory = (value: unknown): string[] => {
  const problems = validateAgainstSchema("code-inventory.v1.json", value);
  if (problems.length > 0) return problems;

  const inventory = value as CodeInventory;
  for (const id of duplicateIDs(inventory.actions.map((action) => action.id))) {
    problems.push(`duplicate action ID ${JSON.stringify(id)}`);
  }
  for (const id of duplicateIDs(inventory.targets.map((target) => target.id))) {
    problems.push(`duplicate target ID ${JSON.stringify(id)}`);
  }

  const actionsByID = new Map(inventory.actions.map((action) => [action.id, action]));
  for (const action of inventory.actions) {
    if (!action.prefix) continue;
    if (action.prefix === action.id) {
      problems.push(`action ID ${JSON.stringify(action.id)} cannot prefix itself`);
      continue;
    }
    const prefix = actionsByID.get(action.prefix);
    if (!prefix) {
      problems.push(`action ID ${JSON.stringify(action.id)} has unknown prefix ${JSON.stringify(action.prefix)}`);
    } else if (prefix.prefix) {
      problems.push(`action ID ${JSON.stringify(action.id)} has nested prefix ${JSON.stringify(action.prefix)}`);
    }
  }
  return problems.sort(compareStrings);
};

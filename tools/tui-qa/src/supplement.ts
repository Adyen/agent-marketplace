import crypto from "node:crypto";
import fs from "node:fs";
import path from "node:path";
import process from "node:process";

import { compareStrings } from "./order.js";
import { validateAgainstSchema } from "./schema.js";
import { sanitizeMultiline, scanValueForSecrets } from "./sanitize.js";

export interface ExploratoryObservation {
  id: string;
  severity: "high" | "medium" | "low" | "note";
  category: "product" | "environment" | "harness" | "coverage" | "usability";
  title: string;
  detail: string;
  flowId?: string;
  proposedFlowId?: string;
  evidencePaths: string[];
}

export interface ExploratorySupplement {
  schemaVersion: 1;
  runId: string;
  resultsSha256: string;
  generatedAt: string;
  agent: { name: string; model: string };
  supplementalOnly: true;
  requestedAdditionalFlowIds: string[];
  observations: ExploratoryObservation[];
}

const normalize = (supplement: ExploratorySupplement): ExploratorySupplement => ({
  ...supplement,
  agent: {
    name: sanitizeMultiline(supplement.agent.name),
    model: sanitizeMultiline(supplement.agent.model)
  },
  requestedAdditionalFlowIds: [...supplement.requestedAdditionalFlowIds].sort(),
  observations: supplement.observations
    .map((observation) => ({
      ...observation,
      title: sanitizeMultiline(observation.title),
      detail: sanitizeMultiline(observation.detail),
      evidencePaths: [...observation.evidencePaths].sort()
    }))
    .sort((left, right) => compareStrings(left.id, right.id))
});

export interface SupplementBinding {
  runId: string;
  resultsSha256: string;
}

export const normalizeSupplement = (value: unknown, binding?: SupplementBinding): ExploratorySupplement => {
  const problems = validateAgainstSchema("exploratory-supplement.v1.json", value);
  if (problems.length > 0) throw new Error(`invalid exploratory supplement: ${problems.join("; ")}`);
  const supplement = normalize(value as ExploratorySupplement);
  const normalizedProblems = validateAgainstSchema("exploratory-supplement.v1.json", supplement);
  if (normalizedProblems.length > 0) throw new Error(`sanitized exploratory supplement invalid: ${normalizedProblems.join("; ")}`);
  const secretFindings = scanValueForSecrets(supplement);
  if (secretFindings.length > 0) throw new Error(`exploratory supplement contains credential-shaped content: ${secretFindings.join(", ")}`);
  if (!Number.isFinite(Date.parse(supplement.generatedAt))) throw new Error("exploratory supplement generatedAt is invalid");
  if (binding && (supplement.runId !== binding.runId || supplement.resultsSha256 !== binding.resultsSha256)) {
    throw new Error("exploratory supplement does not match deterministic results binding");
  }
  const ids = supplement.observations.map((observation) => observation.id);
  if (new Set(ids).size !== ids.length) throw new Error("exploratory supplement observation IDs must be unique");
  return supplement;
};

export const writeSupplement = (file: string, value: unknown, binding?: SupplementBinding): ExploratorySupplement => {
  if (fs.existsSync(file)) throw new Error(`refusing to overwrite immutable supplement: ${file}`);
  const supplement = normalizeSupplement(value, binding);
  fs.mkdirSync(path.dirname(file), { recursive: true });
  const temporary = `${file}.tmp-${process.pid}-${crypto.randomUUID()}`;
  try {
    fs.writeFileSync(temporary, JSON.stringify(supplement, null, 2) + "\n", { encoding: "utf8", flag: "wx", mode: 0o600 });
    try {
      fs.linkSync(temporary, file);
    } catch (error) {
      if ((error as NodeJS.ErrnoException).code === "EEXIST") {
        throw new Error(`refusing to overwrite immutable supplement: ${file}`);
      }
      throw error;
    }
  } finally {
    if (fs.existsSync(temporary)) fs.rmSync(temporary);
  }
  return supplement;
};

export const readSupplement = (file: string, binding?: SupplementBinding): ExploratorySupplement => {
  if (fs.statSync(file).size > 10 * 1024 * 1024) throw new Error("exploratory supplement exceeds 10 MiB limit");
  const value: unknown = JSON.parse(fs.readFileSync(file, "utf8"));
  const normalized = normalizeSupplement(value, binding);
  if (JSON.stringify(value) !== JSON.stringify(normalized)) {
    throw new Error("stored exploratory supplement is not sanitized and canonically ordered");
  }
  return normalized;
};

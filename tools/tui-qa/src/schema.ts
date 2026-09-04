import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

import { Ajv2020, type AnySchema, type ErrorObject, type ValidateFunction } from "ajv/dist/2020.js";

const moduleDir = path.dirname(fileURLToPath(import.meta.url));
export const SCHEMA_FILES = ["code-inventory.v1.json", "coverage-index.v1.json", "exploratory-supplement.v1.json", "results.v1.json"] as const;
export type SchemaFile = (typeof SCHEMA_FILES)[number];

export const schemaDirectory = (): string => {
  const candidates = [
    path.resolve(moduleDir, "..", "schemas"),
    path.resolve(moduleDir, "..", "..", "schemas")
  ];
  for (const candidate of candidates) {
    if (fs.existsSync(candidate) && fs.statSync(candidate).isDirectory()) return candidate;
  }
  throw new Error(`TUI QA schema directory not found; checked ${candidates.join(", ")}`);
};

const ajv = new Ajv2020({
  allErrors: true,
  strict: true,
  validateFormats: false
});
const validators = new Map<SchemaFile, ValidateFunction>();

const validatorFor = (schemaFile: SchemaFile): ValidateFunction => {
  const cached = validators.get(schemaFile);
  if (cached) return cached;
  const schemaPath = path.join(schemaDirectory(), schemaFile);
  const schema = JSON.parse(fs.readFileSync(schemaPath, "utf8")) as AnySchema;
  try {
    const validator = ajv.compile(schema);
    validators.set(schemaFile, validator);
    return validator;
  } catch (error) {
    if (typeof schema === "object" && schema !== null && "$id" in schema && typeof schema.$id === "string") {
      ajv.removeSchema(schema.$id);
    }
    throw error;
  }
};

const formatError = (error: ErrorObject): string => {
  const location = error.instancePath === "" ? "$" : `$${error.instancePath}`;
  const detail = error.message ?? error.keyword;
  return `${location} ${detail}`;
};

export const validateAgainstSchema = (schemaFile: SchemaFile, value: unknown): string[] => {
  const validator = validatorFor(schemaFile);
  if (validator(value)) return [];
  return (validator.errors ?? []).map(formatError).sort();
};

export const assertSchemaValid = (schemaFile: SchemaFile, value: unknown): void => {
  const problems = validateAgainstSchema(schemaFile, value);
  if (problems.length > 0) {
    throw new Error(`${schemaFile} validation failed: ${problems.join("; ")}`);
  }
};

export const validateAllSchemasCompile = (): string[] => {
  const problems: string[] = [];
  for (const schemaFile of SCHEMA_FILES) {
    try {
      validatorFor(schemaFile);
    } catch (error) {
      problems.push(`${schemaFile}: ${error instanceof Error ? error.message : String(error)}`);
    }
  }
  return problems;
};

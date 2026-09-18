import { mkdtemp, mkdir, readFile, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { dirname, resolve } from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";
import openapiTS, { astToString, COMMENT_HEADER } from "openapi-typescript";

const webRoot = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const openapiPath = resolve(webRoot, "..", "api", "openapi.yaml");
const outputPath = resolve(webRoot, "app", "generated", "api.d.ts");
const check = process.argv.includes("--check");

const generated = `${COMMENT_HEADER}${astToString(
  await openapiTS(pathToFileURL(openapiPath), {
    alphabetize: true,
  }),
)}`;

if (!check) {
  await mkdir(dirname(outputPath), { recursive: true });
  await writeFile(outputPath, generated, "utf8");
  console.log(`Generated ${outputPath}`);
  process.exit(0);
}

const temporaryDirectory = await mkdtemp(
  resolve(tmpdir(), "agent-marketplace-openapi-"),
);
const temporaryPath = resolve(temporaryDirectory, "api.d.ts");
try {
  await writeFile(temporaryPath, generated, "utf8");
  const [committed, temporary] = await Promise.all([
    readFile(outputPath),
    readFile(temporaryPath),
  ]);
  if (!committed.equals(temporary)) {
    console.error(
      `OpenAPI generated type drift detected in ${outputPath}. Run pnpm api:generate and commit the result.`,
    );
    process.exitCode = 1;
  } else {
    console.log("OpenAPI generated types are up to date.");
  }
} catch (error) {
  if (
    error &&
    typeof error === "object" &&
    "code" in error &&
    error.code === "ENOENT"
  ) {
    console.error(
      `Missing generated OpenAPI types at ${outputPath}. Run pnpm api:generate and commit the result.`,
    );
    process.exitCode = 1;
  } else {
    throw error;
  }
} finally {
  await rm(temporaryDirectory, { recursive: true, force: true });
}

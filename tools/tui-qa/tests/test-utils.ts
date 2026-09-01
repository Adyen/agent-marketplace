import fs from "node:fs";

export const waitForFile = async (file: string, timeoutMs = 3_000, intervalMs = 10): Promise<void> => {
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    if (fs.existsSync(file)) return;
    await new Promise((resolve) => setTimeout(resolve, intervalMs));
  }
  throw new Error(`file was not created: ${file}`);
};

export const waitForJSONFile = async <T>(file: string, timeoutMs = 3_000, intervalMs = 10): Promise<T> => {
  const deadline = Date.now() + timeoutMs;
  let lastError: unknown;
  while (Date.now() < deadline) {
    if (fs.existsSync(file)) {
      try {
        return JSON.parse(fs.readFileSync(file, "utf8")) as T;
      } catch (error) {
        lastError = error;
      }
    }
    await new Promise((resolve) => setTimeout(resolve, intervalMs));
  }
  throw lastError ?? new Error(`JSON file was not created: ${file}`);
};

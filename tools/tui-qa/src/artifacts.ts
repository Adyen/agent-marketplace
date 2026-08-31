import fs from "node:fs";
import path from "node:path";

import { sha256File } from "./digest.js";
import { compareStrings } from "./order.js";
import { rejectSymlinkEscape } from "./sandbox.js";
import { sanitizeMultiline, scanForSecrets } from "./sanitize.js";

const TRUNCATION_MARKER = "\n[TUI-QA TRUNCATED]\n";
const COPY_CHUNK_BYTES = 64 * 1024;
const MAX_TEXT_SOURCE_BYTES = 10 * 1024 * 1024;
const MAX_UPLOAD_BYTES = 100 * 1024 * 1024;
const MAX_ARTIFACT_FILES = 10_000;

export type EvidenceKind =
  | "sanitized-frame"
  | "trace-summary"
  | "command-log"
  | "driver-log"
  | "side-effect"
  | "process-cleanup"
  | "raw-trace"
  | "other";

export interface EvidenceArtifact {
  path: string;
  sha256: string;
  bytes: number;
  kind: EvidenceKind;
  sanitized: boolean;
  sanitization: "transformed" | "raw";
  localOnly: boolean;
  truncated: boolean;
}

export class ArtifactBudget {
  readonly capBytes: number;
  writtenBytes = 0;
  truncated = false;

  constructor(capBytes: number) {
    if (!Number.isSafeInteger(capBytes) || capBytes <= 0 || capBytes > MAX_UPLOAD_BYTES) {
      throw new Error(`artifact budget must be between 1 and ${MAX_UPLOAD_BYTES} bytes`);
    }
    this.capBytes = capBytes;
  }

  get remainingBytes(): number {
    return Math.max(0, this.capBytes - this.writtenBytes);
  }

  consume(bytes: number, truncated: boolean): void {
    if (!Number.isSafeInteger(bytes) || bytes < 0 || bytes > this.remainingBytes) {
      throw new Error(`invalid artifact budget consumption: ${bytes}`);
    }
    this.writtenBytes += bytes;
    this.truncated ||= truncated;
  }
}

export interface RawPolicy {
  upload: boolean;
  syntheticNoSecret: boolean;
}

export interface CollectOptions {
  sourceDir: string;
  destinationDir: string;
  artifactRoot: string;
  transcriptCapBytes: number;
  budget: ArtifactBudget;
  rawPolicy: RawPolicy;
}

const relativePortable = (from: string, to: string): string => path.relative(from, to).split(path.sep).join("/");

const isWithin = (candidate: string, root: string): boolean => {
  const relative = path.relative(path.resolve(root), path.resolve(candidate));
  return relative === "" || (!path.isAbsolute(relative) && relative !== ".." && !relative.startsWith(`..${path.sep}`));
};

const isRawTrace = (relative: string): boolean => {
  const portable = relative.split(path.sep).join("/");
  return portable === "traces" || portable.startsWith("traces/") || portable.endsWith(".trace");
};

const kindFor = (relative: string, raw: boolean): EvidenceKind => {
  if (raw) return "raw-trace";
  if (relative.endsWith(".snapshot.txt")) return "sanitized-frame";
  if (relative.endsWith("command-log.txt")) return "command-log";
  if (relative.endsWith("driver-log.txt")) return "driver-log";
  if (relative.endsWith("workspace-diff.json") || relative.endsWith("managed-roots-diff.json")) return "side-effect";
  if (relative.includes("process-cleanup")) return "process-cleanup";
  if (relative.includes("trace-summary")) return "trace-summary";
  return "other";
};

const walkFiles = (root: string): Array<{ source: string; relative: string; raw: boolean }> => {
  const files: Array<{ source: string; relative: string; raw: boolean }> = [];
  let entries = 0;
  const walk = (current: string): void => {
    for (const entry of fs.readdirSync(current, { withFileTypes: true }).sort((a, b) => compareStrings(a.name, b.name))) {
      if (++entries > MAX_ARTIFACT_FILES) throw new Error(`artifact entry limit exceeded under ${root}`);
      const full = path.join(current, entry.name);
      const stat = fs.lstatSync(full);
      if (stat.isSymbolicLink()) throw new Error(`artifact symlink rejected: ${full}`);
      if (stat.isDirectory()) walk(full);
      else if (stat.isFile()) {
        const relative = path.relative(root, full);
        files.push({ source: full, relative, raw: isRawTrace(relative) });
      }
    }
  };
  walk(root);
  return files;
};

const fileHasSecret = (file: string): boolean => {
  const descriptor = fs.openSync(file, "r");
  try {
    const chunk = Buffer.allocUnsafe(COPY_CHUNK_BYTES);
    let carry = "";
    for (;;) {
      const count = fs.readSync(descriptor, chunk, 0, chunk.length, null);
      if (count === 0) return scanForSecrets(carry).length > 0;
      const text = carry + chunk.subarray(0, count).toString("utf8");
      if (scanForSecrets(text).length > 0) return true;
      carry = text.slice(-1_024);
    }
  } finally {
    fs.closeSync(descriptor);
  }
};

const trimIncompleteUTF8 = (value: Buffer): Buffer => {
  let start = value.length - 1;
  while (start >= 0 && (value[start] & 0xc0) === 0x80) start--;
  if (start < 0) return Buffer.alloc(0);
  const lead = value[start];
  const expected = lead >= 0xf0 && lead <= 0xf4 ? 4 : lead >= 0xe0 && lead <= 0xef ? 3 : lead >= 0xc2 && lead <= 0xdf ? 2 : 1;
  return value.length - start < expected ? value.subarray(0, start) : value;
};

const boundedContent = (content: Buffer, limit: number, forceTruncated: boolean): { content: Buffer; truncated: boolean } => {
  const marker = Buffer.from(TRUNCATION_MARKER);
  const truncated = forceTruncated || content.length > limit;
  if (!truncated) return { content, truncated: false };
  const markerBytes = Math.min(marker.length, limit);
  const payloadBytes = Math.max(0, limit - markerBytes);
  const payload = trimIncompleteUTF8(content.subarray(0, payloadBytes));
  return { content: Buffer.concat([payload, marker.subarray(0, limit - payload.length)]), truncated: true };
};

const readPrefix = (file: string, limit: number): { bytes: Buffer; truncated: boolean } => {
  const size = fs.statSync(file).size;
  const readLimit = Math.min(size, limit);
  const descriptor = fs.openSync(file, "r");
  try {
    const bytes = Buffer.alloc(readLimit);
    let offset = 0;
    while (offset < readLimit) {
      const count = fs.readSync(descriptor, bytes, offset, readLimit - offset, null);
      if (count === 0) break;
      offset += count;
    }
    return { bytes: bytes.subarray(0, Math.min(offset, limit)), truncated: size > limit };
  } finally {
    fs.closeSync(descriptor);
  }
};

const writeExclusive = (destination: string, content: Buffer): void => {
  fs.mkdirSync(path.dirname(destination), { recursive: true });
  fs.writeFileSync(destination, content, { flag: "wx", mode: 0o600 });
};

const copyRawBounded = (source: string, destination: string, limit: number): { bytes: number; truncated: boolean } => {
  const prefix = readPrefix(source, limit);
  const bounded = boundedContent(prefix.bytes, limit, prefix.truncated);
  writeExclusive(destination, bounded.content);
  return { bytes: bounded.content.length, truncated: bounded.truncated };
};

const copySanitizedBounded = (source: string, destination: string, limit: number): { bytes: number; truncated: boolean } => {
  const prefix = readPrefix(source, Math.min(limit, MAX_TEXT_SOURCE_BYTES));
  const sanitized = Buffer.from(sanitizeMultiline(prefix.bytes.toString("utf8")), "utf8");
  if (scanForSecrets(sanitized.toString("utf8")).length > 0) {
    throw new Error(`sanitized artifact still contains credential-shaped content: ${source}`);
  }
  const bounded = boundedContent(sanitized, limit, prefix.truncated);
  writeExclusive(destination, bounded.content);
  return { bytes: bounded.content.length, truncated: bounded.truncated };
};

export const boundLocalRawTraces = (traceDir: string, capBytes: number): { bytes: number; truncated: boolean } => {
  if (!Number.isSafeInteger(capBytes) || capBytes <= 0 || capBytes > MAX_TEXT_SOURCE_BYTES) {
    throw new Error(`raw trace cap must be between 1 and ${MAX_TEXT_SOURCE_BYTES} bytes`);
  }
  if (!fs.existsSync(traceDir)) return { bytes: 0, truncated: false };
  const files = walkFiles(traceDir).map((file) => file.source);
  let remaining = capBytes;
  let bytes = 0;
  let truncated = false;
  for (const file of files) {
    const size = fs.statSync(file).size;
    if (size <= remaining) {
      remaining -= size;
      bytes += size;
      continue;
    }
    truncated = true;
    const prefix = readPrefix(file, remaining);
    const bounded = boundedContent(prefix.bytes, remaining, true);
    fs.writeFileSync(file, bounded.content, { mode: 0o600 });
    bytes += bounded.content.length;
    remaining = 0;
  }
  if (truncated) {
    const summary = path.join(path.dirname(traceDir), "trace-summary-truncated.txt");
    const content = `raw transcript truncated at ${capBytes} bytes\n`;
    try {
      fs.writeFileSync(summary, content, { encoding: "utf8", flag: "wx", mode: 0o600 });
    } catch (error) {
      if ((error as NodeJS.ErrnoException).code !== "EEXIST" || fs.readFileSync(summary, "utf8") !== content) throw error;
    }
  }
  return { bytes, truncated };
};

export const collectAttemptArtifacts = (options: CollectOptions): EvidenceArtifact[] => {
  if (!Number.isSafeInteger(options.transcriptCapBytes) ||
      options.transcriptCapBytes <= 0 ||
      options.transcriptCapBytes > MAX_TEXT_SOURCE_BYTES) {
    throw new Error(`transcript cap must be between 1 and ${MAX_TEXT_SOURCE_BYTES} bytes`);
  }
  if (!isWithin(options.destinationDir, options.artifactRoot)) {
    throw new Error(`artifact destination escapes root: ${options.destinationDir}`);
  }
  if (!fs.existsSync(options.sourceDir)) return [];
  if (fs.lstatSync(options.sourceDir).isSymbolicLink()) throw new Error(`artifact source symlink rejected: ${options.sourceDir}`);
  rejectSymlinkEscape(options.destinationDir, options.artifactRoot);

  const evidence: EvidenceArtifact[] = [];
  let transcriptRemaining = options.transcriptCapBytes;
  for (const file of walkFiles(options.sourceDir)) {
    if (file.raw) {
      if (!options.rawPolicy.upload || !options.rawPolicy.syntheticNoSecret) continue;
      if (fileHasSecret(file.source)) throw new Error(`secret scan rejected exceptional raw artifact: ${file.relative}`);
    }
    const perFlowRemaining = file.raw ? transcriptRemaining : Number.MAX_SAFE_INTEGER;
    const limit = Math.min(perFlowRemaining, options.budget.remainingBytes);
    if (limit <= 0) {
      options.budget.truncated = true;
      continue;
    }

    const destination = path.join(options.destinationDir, file.relative);
    if (!isWithin(destination, options.destinationDir)) throw new Error(`artifact path escapes destination: ${file.relative}`);
    rejectSymlinkEscape(destination, options.artifactRoot);
    const copied = file.raw
      ? copyRawBounded(file.source, destination, limit)
      : copySanitizedBounded(file.source, destination, limit);
    options.budget.consume(copied.bytes, copied.truncated);
    if (file.raw) transcriptRemaining -= copied.bytes;
    evidence.push({
      path: relativePortable(options.artifactRoot, destination),
      sha256: sha256File(destination),
      bytes: copied.bytes,
      kind: kindFor(file.relative, file.raw),
      sanitized: !file.raw,
      sanitization: file.raw ? "raw" : "transformed",
      localOnly: false,
      truncated: copied.truncated
    });
  }
  return evidence.sort((left, right) => compareStrings(left.path, right.path));
};

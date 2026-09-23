import { randomBytes } from "node:crypto";

export const basePolicy = [
  "default-src 'self'",
  "base-uri 'self'",
  "object-src 'none'",
  "frame-ancestors 'none'",
  "form-action 'self'",
  "style-src 'self' 'unsafe-inline'",
  "font-src 'self' data:",
];

export function generateCspNonce() {
  return randomBytes(16).toString("base64url");
}

export function imagePolicy(sources: string[]) {
  return `img-src ${sources.join(" ")}`;
}

export function scriptPolicy(nonce: string) {
  return `script-src 'self' 'nonce-${nonce}'`;
}

export function addNonceToScripts(markup: string, nonce: string) {
  return markup.replace(
    /<script(?![^>]*(?:\bsrc\s*=|\bnonce\s*=))(?=\s|>)/gi,
    `<script nonce="${nonce}"`,
  );
}

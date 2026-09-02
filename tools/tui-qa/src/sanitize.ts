const ESC = String.fromCharCode(27);
const BEL = String.fromCharCode(7);

const CONTROL_REPLACEMENTS: Record<string, string> = {
  "\r": "\\r",
  "\n": "\\n",
  "\t": "\\t"
};

const BIDI_RE = /[؜‎‏‪-‮⁦-⁩]/g;
const ANSI_RE = new RegExp(
  ESC + "(?:\\[[0-?]*[ -/]*[@-~]|\\][^" + BEL + "]*(?:" + BEL + "|" + ESC + "\\\\)|[@-_])",
  "g"
);

const isControl = (value: string): boolean => {
  if (CONTROL_REPLACEMENTS[value] !== undefined) return false;
  const code = value.charCodeAt(0);
  return code < 32 || (code >= 127 && code <= 159);
};

export const escapeTerminal = (value: string): string => {
  let escaped = value.replace(ANSI_RE, (sequence) => `\\e${sequence.slice(1).replace(/\n/g, "\\n")}`);
  escaped = escaped
    .split("")
    .map((character) => {
      if (CONTROL_REPLACEMENTS[character] !== undefined) return CONTROL_REPLACEMENTS[character];
      if (isControl(character)) return `\\x${character.charCodeAt(0).toString(16).padStart(2, "0")}`;
      return character;
    })
    .join("");
  return escaped.replace(BIDI_RE, (character) => `\\u${character.codePointAt(0)!.toString(16).padStart(4, "0")}`);
};

const CREDENTIAL_RE = /:\/\/[^/\s?#]+@/g;
const CREDENTIAL_ASSIGNMENT_RE = /\b(token|password|passwd|secret|api[_-]?key|authorization)\s*[:=]\s*([^\s,;]+)/gi;
const BEARER_RE = /\bBearer\s+[A-Za-z0-9._~+\/-]+=*/gi;
const PRIVATE_KEY_BLOCK_RE = /-----BEGIN ([A-Z ]*PRIVATE KEY)-----[\s\S]*?-----END \1-----/g;
const GITHUB_TOKEN_RE = /\bgh[pousr]_[A-Za-z0-9]{20,}\b/g;
const AWS_ACCESS_KEY_RE = /\b(?:AKIA|ASIA)[A-Z0-9]{16}\b/g;
const KNOWN_SECRET_PATTERNS: Array<[string, RegExp]> = [
  ["private-key", /-----BEGIN (?:[A-Z ]+ )?PRIVATE KEY-----/],
  ["github-token", /\bgh[pousr]_[A-Za-z0-9]{20,}\b/],
  ["aws-access-key", /\b(?:AKIA|ASIA)[A-Z0-9]{16}\b/],
  ["bearer-token", /\bBearer\s+[A-Za-z0-9._~+\/-]{12,}=*/i],
  ["credential-assignment", /\b(?:token|password|passwd|secret|api[_-]?key|authorization)\s*[:=]\s*[^\s,;]{4,}/i]
];

export const redactURLCredentials = (value: string): string => value.replace(CREDENTIAL_RE, "://***@");

export const scanForSecrets = (value: string): string[] =>
  KNOWN_SECRET_PATTERNS.filter(([, pattern]) => pattern.test(value)).map(([name]) => name);

export const scanValueForSecrets = (value: unknown): string[] => {
  const findings = new Set<string>();
  const scan = (nested: unknown): void => {
    if (typeof nested === "string") {
      for (const finding of scanForSecrets(nested)) findings.add(finding);
    } else if (Array.isArray(nested)) {
      for (const item of nested) scan(item);
    } else if (typeof nested === "object" && nested !== null) {
      for (const item of Object.values(nested as Record<string, unknown>)) scan(item);
    }
  };
  scan(value);
  return [...findings].sort();
};

export const redactCredentialShaped = (value: string): string =>
  redactURLCredentials(value)
    .replace(PRIVATE_KEY_BLOCK_RE, "[REDACTED PRIVATE KEY]")
    .replace(GITHUB_TOKEN_RE, "[REDACTED TOKEN]")
    .replace(AWS_ACCESS_KEY_RE, "[REDACTED TOKEN]")
    .replace(BEARER_RE, "Bearer ***")
    .replace(CREDENTIAL_ASSIGNMENT_RE, (_match, key: string) => `${key}=***`);

export const ALLOWED_ENV_KEYS = [
  "AM_TUI_QA_RUN_ID",
  "AM_TUI_QA_RUN_CLASS",
  "AM_TUI_QA_COLUMNS",
  "AM_TUI_QA_ROWS",
  "AM_TUI_QA_RETRIES",
  "AM_TUI_QA_TIMEOUT_MS",
  "GOOS",
  "GOARCH",
  "TERM"
] as const;

export const allowedEnv = (env: NodeJS.ProcessEnv): Record<string, string> => {
  const allowed: Record<string, string> = {};
  for (const key of ALLOWED_ENV_KEYS) {
    const value = env[key];
    if (value !== undefined) allowed[key] = redactCredentialShaped(escapeTerminal(value));
  }
  return allowed;
};

export const sanitizeMultiline = (value: string): string =>
  redactCredentialShaped(value).split(/\r?\n/).map((line) => escapeTerminal(line)).join("\n");

export const markdownInline = (value: string): string =>
  redactCredentialShaped(escapeTerminal(value)).replace(/[\\`*_[\]()<>|]/g, (character) => `\\${character}`);

export const markdownCodeFence = (value: string): string => {
  const escaped = sanitizeMultiline(value).replace(/```/g, "\\`\\`\\`");
  return `\`\`\`text\n${escaped}\n\`\`\``;
};

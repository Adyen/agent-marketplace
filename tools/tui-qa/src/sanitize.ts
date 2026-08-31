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

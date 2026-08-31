import { TARGET_EXIT_BOUNDARY_VERSION } from "./protocol.js";

const ESC = String.fromCharCode(27);
const TARGET_EXIT_BOUNDARY = /\nAM_TUI_QA_TARGET_EXIT_BOUNDARY version=(\d+) code=(?:null|-?\d+) signal=(?:none|[A-Z0-9]+)\n/g;
const DEC_MODE = new RegExp(`${ESC}\\[\\?([0-9]+(?:;[0-9]+)*)([hl])`, "g");

export interface TraceSegments {
  target: string;
  harness: string;
}

export interface TermiosState {
  canonical: boolean | null;
  echo: boolean | null;
}

export interface RestorationAssessment {
  alternateScreen: boolean;
  cursorVisible: boolean;
  mouseDisabled: boolean;
  bracketedPasteDisabled: boolean;
  canonical: boolean;
  echo: boolean;
  complete: boolean;
}

interface ModePositions {
  set: number;
  reset: number;
}

export const splitTargetTrace = (raw: string): TraceSegments => {
  const matches = [...raw.matchAll(TARGET_EXIT_BOUNDARY)];
  const unsupported = matches.find((match) => Number(match[1]) !== TARGET_EXIT_BOUNDARY_VERSION);
  if (unsupported) {
    throw new Error(`unsupported target-exit boundary version ${unsupported[1]}; expected ${TARGET_EXIT_BOUNDARY_VERSION}`);
  }
  if (matches.length !== 1) throw new Error(`expected exactly one target-exit boundary, found ${matches.length}`);
  const match = matches[0];
  const index = match.index;
  if (index === undefined) throw new Error("target-exit boundary offset missing");
  return { target: raw.slice(0, index), harness: raw.slice(index + match[0].length) };
};

const modePositions = (value: string): Map<number, ModePositions> => {
  const positions = new Map<number, ModePositions>();
  for (const match of value.matchAll(DEC_MODE)) {
    const index = match.index ?? -1;
    for (const rawMode of match[1].split(";")) {
      const mode = Number(rawMode);
      const current = positions.get(mode) ?? { set: -1, reset: -1 };
      if (match[2] === "h") current.set = index;
      else current.reset = index;
      positions.set(mode, current);
    }
  }
  return positions;
};

const restoredMode = (
  positions: Map<number, ModePositions>,
  mode: number,
  finalState: "set" | "reset",
  requireOpposite: boolean
): boolean => {
  const current = positions.get(mode);
  if (!current) return !requireOpposite;
  if (finalState === "reset") {
    if (current.set < 0) return !requireOpposite;
    return current.reset > current.set;
  }
  if (current.reset < 0) return !requireOpposite;
  return current.set > current.reset;
};

export const assessProductRestoration = (target: string, termios: TermiosState): RestorationAssessment => {
  const positions = modePositions(target);
  const alternateScreen = restoredMode(positions, 1049, "reset", true);
  const cursorVisible = restoredMode(positions, 25, "set", true);
  const mouseDisabled = [1000, 1002, 1003, 1006].every((mode) => restoredMode(positions, mode, "reset", false));
  const bracketedPasteDisabled = restoredMode(positions, 2004, "reset", false);
  const canonical = termios.canonical === true;
  const echo = termios.echo === true;
  return {
    alternateScreen,
    cursorVisible,
    mouseDisabled,
    bracketedPasteDisabled,
    canonical,
    echo,
    complete: alternateScreen && cursorVisible && mouseDisabled && bracketedPasteDisabled && canonical && echo
  };
};

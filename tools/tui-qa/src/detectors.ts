import type { FixtureSessionResult } from "./fixture-session.js";
import { TERMINAL_RESET_SEQUENCE } from "./protocol.js";
import type { FailureClass } from "./results.js";
import { assessProductRestoration } from "./trace.js";

const BIDI_CONTROL = /[؜‎‏‪-‮⁦-⁩]/;
const OSC_INJECTION = /(?:\u001b\]|\u009d)(?:8|52);/;

export const detectsClippedChrome = (
  result: Pick<FixtureSessionResult, "targetRaw" | "targetFrame">,
  visibleAnchor: string,
  clippedAnchor: string
): boolean => {
  const frame = result.targetFrame.join("\n");
  return result.targetRaw.includes(clippedAnchor) && frame.includes(visibleAnchor) && !frame.includes(clippedAnchor);
};

export const detectsBlankFrame = (result: Pick<FixtureSessionResult, "targetFrame">): boolean =>
  result.targetFrame.every((row) => row.trim() === "");

export const detectsControlInjection = (result: Pick<FixtureSessionResult, "targetRaw">): boolean =>
  OSC_INJECTION.test(result.targetRaw) || BIDI_CONTROL.test(result.targetRaw);

export const productRestorationComplete = (
  result: Pick<FixtureSessionResult, "targetRaw" | "lifecycle">
): boolean => {
  const target = result.lifecycle.targetAtExit;
  if (!target.probeOK) return false;
  return assessProductRestoration(result.targetRaw, { canonical: target.canonical, echo: target.echo }).complete;
};

export const harnessRecoveryComplete = (
  result: Pick<FixtureSessionResult, "harnessRaw" | "lifecycle" | "cleanup">
): boolean => {
  const recovery = result.lifecycle.harnessRecovery;
  return recovery.applied &&
    recovery.probeOK &&
    recovery.canonical === true &&
    recovery.echo === true &&
    result.harnessRaw.includes(TERMINAL_RESET_SEQUENCE) &&
    result.cleanup.complete;
};

export const classifyFixtureExit = (
  result: Pick<FixtureSessionResult, "timedOut" | "targetRaw" | "exitCode" | "lifecycle">
): FailureClass | null => {
  if (result.timedOut) return "HANG_TIMEOUT";
  if (result.lifecycle.signal === "SIGKILL") return "SIGKILL";
  if (result.targetRaw.includes("panic: synthetic fixture failure")) return "PANIC";
  if (result.targetRaw.includes("application returned error: synthetic failure")) return "RETURNED_ERROR";
  return result.exitCode === 0 ? null : "UNEXPECTED_EXIT";
};

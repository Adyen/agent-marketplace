import type { FailureClass } from "./results.js";

export interface DetectorSpec {
  id: string;
  fixture: string;
  expectedFailureClass: FailureClass;
}

export const DETECTOR_SPECS: readonly DetectorSpec[] = [
  { id: "detector-blank", fixture: "broken-blank.sh", expectedFailureClass: "BLANK_SCREEN" },
  { id: "detector-clip", fixture: "broken-clip.sh", expectedFailureClass: "CLIPPED_CHROME" },
  { id: "detector-exit", fixture: "broken-exit.sh", expectedFailureClass: "UNEXPECTED_EXIT" },
  { id: "detector-hang", fixture: "broken-hang.sh", expectedFailureClass: "HANG_TIMEOUT" },
  { id: "detector-inject", fixture: "broken-inject.sh", expectedFailureClass: "CONTROL_INJECTION" },
  { id: "detector-leaked-child", fixture: "broken-leaked-child.sh", expectedFailureClass: "LEAKED_DESCENDANT" },
  { id: "detector-panic", fixture: "broken-panic.sh", expectedFailureClass: "PANIC" },
  { id: "detector-restore", fixture: "broken-restore.sh", expectedFailureClass: "TERMINAL_RESTORATION" },
  { id: "detector-returned-error", fixture: "broken-return-error.sh", expectedFailureClass: "RETURNED_ERROR" },
  { id: "detector-sigkill", fixture: "broken-sigkill.sh", expectedFailureClass: "SIGKILL" }
];

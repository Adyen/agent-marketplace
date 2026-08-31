// Deterministic result status model for the TUI QA workbench.
//
// Per-flow statuses and final aggregation are defined by the
// `tui-qa-results` capability. Do not extend without updating the
// versioned schema and this file together.

export const FLOW_STATUSES = [
  "PASS",
  "FAIL",
  "BLOCKED",
  "FLAKY",
  "SKIP",
  "NOT_APPLICABLE"
] as const;
export type FlowStatus = (typeof FLOW_STATUSES)[number];

export const FINAL_STATUSES = [
  "INVALID",
  "FAIL",
  "BLOCKED",
  "PASS",
  "NOT_APPLICABLE"
] as const;
export type FinalStatus = (typeof FINAL_STATUSES)[number];

export interface FlowResult {
  flowId: string;
  required: boolean;
  status: FlowStatus;
  reason?: string;
}

export interface AggregateOptions {
  invalid?: boolean;
}

/**
 * aggregate implements the contract:
 *   INVALID when the configuration cannot be trusted;
 *   FAIL when any required flow is FAIL or FLAKY;
 *   BLOCKED when any required flow is BLOCKED (and none failed);
 *   PASS when every selected required flow is PASS;
 *   NOT_APPLICABLE when no required flow applies.
 * Optional SKIP/BLOCKED stay visible but never change the final status.
 */
export function aggregate(flows: FlowResult[], options: AggregateOptions = {}): FinalStatus {
  if (options.invalid) {
    return "INVALID";
  }
  const required = flows.filter((flow) => flow.required);
  if (required.some((flow) => flow.status === "FAIL" || flow.status === "FLAKY")) {
    return "FAIL";
  }
  if (required.some((flow) => flow.status === "BLOCKED")) {
    return "BLOCKED";
  }
  const applicable = required.filter((flow) => flow.status !== "NOT_APPLICABLE");
  if (applicable.length === 0) return "NOT_APPLICABLE";
  if (applicable.every((flow) => flow.status === "PASS")) return "PASS";
  return "BLOCKED";
}

/** exitCodeForStatus maps final status to process exit codes: 0/1/2/3. */
export function exitCodeForStatus(status: FinalStatus): number {
  switch (status) {
    case "PASS":
    case "NOT_APPLICABLE":
      return 0;
    case "FAIL":
      return 1;
    case "BLOCKED":
      return 2;
    case "INVALID":
      return 3;
  }
}

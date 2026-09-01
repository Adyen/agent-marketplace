export type InvariantAction =
  | "help"
  | "org-fallback"
  | "quit-ctrl-c"
  | "quit-q"
  | "quit-sigterm"
  | "render-and-quit"
  | "tui-without-tty";

export interface InvariantFlowSpec {
  id: string;
  driver: "pty" | "command";
  required: true;
  description: string;
  args: string[];
  columns: 50 | 80;
  rows: 12 | 24;
  action: InvariantAction;
  env?: Record<string, string>;
}

export const INVARIANT_FLOWS: readonly InvariantFlowSpec[] = [
  {
    id: "bare-non-tty-help",
    driver: "command",
    required: true,
    description: "bare invocation without a TTY prints help and exits cleanly",
    args: [],
    columns: 80,
    rows: 24,
    action: "help"
  },
  {
    id: "frame-baseline-80x24",
    driver: "pty",
    required: true,
    description: "the first nonblank baseline frame fits 80x24 with trusted chrome",
    args: ["tui"],
    columns: 80,
    rows: 24,
    action: "render-and-quit"
  },
  {
    id: "frame-constrained-50x12",
    driver: "pty",
    required: true,
    description: "the approved minimum frame fits 50x12 with required keys visible",
    args: ["tui"],
    columns: 50,
    rows: 12,
    action: "render-and-quit"
  },
  {
    id: "launch-bare-tty",
    driver: "pty",
    required: true,
    description: "bare invocation on a TTY launches the TUI",
    args: [],
    columns: 80,
    rows: 24,
    action: "render-and-quit"
  },
  {
    id: "launch-explicit-tui",
    driver: "pty",
    required: true,
    description: "explicit tui invocation on a TTY launches the TUI",
    args: ["tui"],
    columns: 80,
    rows: 24,
    action: "render-and-quit"
  },
  {
    id: "no-tui-env",
    driver: "pty",
    required: true,
    description: "AGENT_MARKETPLACE_NO_TUI suppresses bare TTY launch",
    args: [],
    columns: 80,
    rows: 24,
    action: "help",
    env: { AGENT_MARKETPLACE_NO_TUI: "1" }
  },
  {
    id: "no-tui-flag",
    driver: "pty",
    required: true,
    description: "--no-tui suppresses bare TTY launch",
    args: ["--no-tui"],
    columns: 80,
    rows: 24,
    action: "help"
  },
  {
    id: "org-fallback",
    driver: "command",
    required: true,
    description: "the fixed organization config supplies fallback harnesses inside containment",
    args: ["status"],
    columns: 80,
    rows: 24,
    action: "org-fallback"
  },
  {
    id: "quit-ctrl-c",
    driver: "pty",
    required: true,
    description: "Ctrl+C exits and restores the terminal",
    args: ["tui"],
    columns: 80,
    rows: 24,
    action: "quit-ctrl-c"
  },
  {
    id: "quit-q",
    driver: "pty",
    required: true,
    description: "q exits and restores the terminal",
    args: ["tui"],
    columns: 80,
    rows: 24,
    action: "quit-q"
  },
  {
    id: "quit-sigterm",
    driver: "pty",
    required: true,
    description: "SIGTERM exits and restores the terminal",
    args: ["tui"],
    columns: 80,
    rows: 24,
    action: "quit-sigterm"
  },
  {
    id: "tui-without-tty",
    driver: "command",
    required: true,
    description: "explicit tui without a usable terminal fails fast and does not hang",
    args: ["tui"],
    columns: 80,
    rows: 24,
    action: "tui-without-tty"
  }
];

export const invariantFlow = (id: string): InvariantFlowSpec | undefined => INVARIANT_FLOWS.find((flow) => flow.id === id);

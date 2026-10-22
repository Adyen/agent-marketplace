## Context

Serialized TUI operations (`operationMutation`, `operationEditor`, …) hold the operation serializer until `Update` accepts a completion message with their ID; `operationState.accepts(0)` is false by design. `correlateOperation` stamps IDs on the message a command returns. The editor command returns Bubble Tea's exec message, and Bubble Tea later calls the exec callback, whose `editConfigPathMsg` never passed through `correlateOperation`.

## Goals / Non-Goals

**Goals:** the editor's exit message always completes its operation; a regression test that goes through the real key path and exec callback.

**Non-Goals:** changing how serialized operations or diagnostics work generally.

## Decisions

- **The command owns its operation.** `openEditorCmd(path, operation)` builds both messages with the ID, so no wrapper needs to see a message produced later by Bubble Tea. The `correlateOperation` case for this message becomes unreachable and is removed.
- **Seam at the process boundary only.** `var execEditor = tea.Exec` is the smallest change that lets a test capture the callback. Bubble Tea's exec message is unexported, so the callback cannot be reached otherwise. The test still drives `E` and `p` through `Update` and feeds the callback's message back.
- **Journey order.** The journey runs the failing editor first. A later successful editor operation supersedes that failure, so the drift footer returns and the post-editor toggle can be asserted against `~ in sync`.

## Risks / Trade-offs

- [Another exec-based command could repeat the mistake] → `openEditorCmd` is the only `tea.Exec` use in `internal/tui`.

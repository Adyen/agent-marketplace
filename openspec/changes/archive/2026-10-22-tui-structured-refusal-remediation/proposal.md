## Why

The TUI errors screen rendered the update refusal for a selection that stops resolving (#738) as one line with literal `\x0A` escapes, because every diagnostic is sanitized as a single line. Because the refusal was not remediable, the screen then appended the generic operation guidance, which partly contradicted the refusal's own remediation (#833). Decision on #833: render it as structured remediation and keep single-line sanitization for untrusted text.

## What Changes

- The TUI recognizes errors that offer `DiagnosticCause()` and `DiagnosticRemediation()`. The errors screen shows the cause (keeping any wrapping prefix such as `update refresh failed:`) as the diagnostic and the remediation as its recovery line, each sanitized single-line. The footer shows the same cause.
- The update refusal implements both, built from the same lines as its CLI text. The cause is each miss and load note, joined with `; `; the remediation is each miss's remediation and the closing line. Its `Error()` text and the CLI output are unchanged, because the CLI's hint lookup does not use these methods.
- `tui-error-recovery` adds "Structured refusals render their own remediation".

## Capabilities

### New Capabilities

### Modified Capabilities
- `tui-error-recovery`: structured refusals render their own remediation.

## Impact

`internal/tui` errors screen and footer, and `cmd/agent-marketplace` update refusal. Other errors render as before.

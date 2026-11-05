## Why

Most command errors say what went wrong but not how to fix it. A user who sees `v4.marketplace-lock-mismatch: awesome-ai/collection/coderabbit-skills (line 7): resource "awesome-ai/collection/coderabbit-skills" is not in the lock` is not told which file line 7 is in, what "the lock" is, or which command records the resource. Only a few warnings and the TUI errors screen offer next steps today, and CLI command errors and `doctor` configuration errors offer none.

## What Changes

- Every error printed by a CLI command is followed by at least one `hint:` line with a concrete next step. Specific remediation wins; otherwise the hint comes from the error's stable diagnostic code; otherwise a command-aware fallback names a diagnostic command.
- `doctor` prints a `hint:` line under each configuration and organization error.
- Every stable consumer diagnostic code has a specific hint that names a supported command or concrete action, filled with the file, layer scope, or resource when known. Organization-owned failures direct the user to their administrator and never suggest editing organization files.
- Consumer configuration validation errors name the authored file and line and describe the problem in plain words. Diagnostic codes do not change.
- The TUI errors screen shows the same code hints before its category fallback.
- A writing `sync` or `lock` records Marketplace resources that the configuration selects but an existing acquisition lock does not record, so the hint for that error has a command that fixes it. Existing pins stay unchanged.
- Machine-readable output, error identity, and exit status do not change. Warning remediation keeps its `→` lines.

## Capabilities

### New Capabilities

### Modified Capabilities
- `command-error-output`: printed command and `doctor` errors carry a next-step hint, and configuration validation errors name their file and line.
- `consumer-config-format`: version refusal guidance is printed on the hint line, and other diagnostics get their own code-specific guidance.
- `tui-error-recovery`: the errors screen uses code-specific guidance before category fallbacks.
- `deployed-acquisition-locks`: writing commands record selected Marketplace resources missing from an existing lock.

## Impact

CLI command error output, `doctor` output, consumer configuration validation messages, the TUI errors screen, and the diagnostics, configuration, and troubleshooting reference docs. Writing `sync` and `lock` can now add missing Marketplace resource records to an existing lock. No configuration, JSON, or exit-code behavior changes.

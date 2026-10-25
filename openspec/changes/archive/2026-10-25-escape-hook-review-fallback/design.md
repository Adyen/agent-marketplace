## Context

The fallback review displays compact JSON for hook handlers without a rendered command or prompt. JSON permits some terminal controls as literal Unicode, so compacting alone does not make it safe to display.

## Decision

Use the existing quoted-text escaping strategy and restore only printable JSON backslashes and quotes. Preserve stored payloads and keep the shared formatter responsible for both CLI and TUI reviews.

## Validation

Exercise literal C1 and bidi controls, already-escaped text, quotes, and printable Unicode. Compare formatter benchmarks before and after the change and verify an actual HTTP hook installation and terminal review.

## ADDED Requirements

### Requirement: Hook timeouts stay exact within documented bounds

A hook timeout SHALL be emitted as the exact integer in the target's unit. A zero timeout SHALL be emitted unchanged wherever the target emits timeouts and declares no minimum for the event. Documented per-event bounds SHALL apply first: a timeout below a per-event minimum SHALL be raised to it, and one above a per-event maximum lowered to it, with the warning the target uses for its per-event cap. A timeout whose value in the target's unit still exceeds 9007199254740991 (2^53-1), the largest integer every JSON reader decodes exactly, SHALL be refused with the target's timeout-unsupported error and no artifact. A seconds-to-milliseconds conversion SHALL refuse before it can exceed that value or overflow. Refusal diagnostics SHALL NOT quote the hook payload.

#### Scenario: Zero timeouts pass through unchanged

- **WHEN** a hook with timeout `0` is compiled for Claude Code, Factory Droid, Cursor, Gemini CLI, Devin CLI or Codex `PreToolUse`
- **THEN** the emitted timeout MUST be `0` and no error MUST be reported

#### Scenario: Timeouts below a documented minimum are raised

- **WHEN** a Codex `SessionEnd` hook with timeout `0` is compiled for Codex
- **THEN** the emitted timeout MUST be `1` and the warning `compile.codex.field-dropped` MUST be reported
- **AND** a Codex `SessionEnd` timeout of `9007199254740992` MUST be lowered to `3` with the same warning rather than refused

#### Scenario: The largest exact timeout is emitted exactly

- **WHEN** a hook with timeout `9007199254740991` is compiled for Claude Code, Factory Droid, Cursor, Devin CLI or Codex, or a hook with timeout `9007199254740` seconds is compiled for Gemini CLI
- **THEN** the emitted timeout MUST be exactly `9007199254740991`, or `9007199254740000` milliseconds for Gemini CLI

#### Scenario: Timeouts beyond the exact integer range are refused

- **WHEN** a hook with timeout `9007199254740992` is compiled for Claude Code, Factory Droid, Cursor, Devin CLI or Codex, or a hook with timeout `9007199254741` or `9223372036854776` seconds is compiled for Gemini CLI
- **THEN** no artifact MUST be emitted and the error `hook.timeout-unsupported` MUST be reported without the payload
- **AND** when the same hook is synced to Claude Code and Gemini CLI together with a sibling hook, Claude Code MUST receive the exact timeout, Gemini CLI MUST refuse only that hook, and the sibling MUST install on both

### Requirement: Ambiguous hook sources fail closed

A registry whose hook source declares a modeled hook field twice, including a case-variant spelling, SHALL fail to load with `json.duplicate-key`. `sync` and `sync --check` SHALL refuse before materializing anything: every installed hook file SHALL keep its bytes, including hooks that registry installed earlier and hooks from other registries, and the refusal SHALL NOT quote any payload outside the hook review. Correcting the source SHALL let the corrected hook install.

#### Scenario: Ambiguous hook sources refuse without changing output

- **WHEN** a selected hook's handler in an installed registry gains a case-variant duplicate of `command` and `sync` or `sync --check` runs, on any harness target
- **THEN** the command MUST fail with `json.duplicate-key` and every installed hook file MUST keep its bytes
- **AND** after the source is corrected, `sync` MUST install the corrected hook and keep the other registry's hook

## Context

The resolver already has a POSIX argument formatter used for structured collision guidance. Prerequisite hints instead use Go `%q`, which permits shell interpretation inside double quotes.

## Decisions

1. Reuse the existing formatter for all three prerequisite-authored string arguments: resource name, redacted registry and redacted path. Do not introduce a second quoting algorithm.
2. Keep warning type/message, resolution and selection unchanged. Ordinary command arguments may lose unnecessary display quotes while remaining the same shell arguments.
3. Test emitted text exactly through the public remediation method and registry-authored prerequisite resolution. Fixtures contain inert syntax and do not execute any command.
4. Scope is the confirmed prerequisite-hint case only, not every remediation/output sink or full R3 closure.

## Verification

The literal-syntax regression failed before correction. Actual Resolve preserves selected resource and typed unmet warning while producing a literal hint. Existing ordinary warnings, redaction, location controls, command/E2E gates, scalar benchmark and adversarial review remain required.

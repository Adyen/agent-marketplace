## Why

Unmet prerequisite remediation currently uses Go string quoting for registry-authored name and location values. Double-quoted shell arguments still interpret command and variable substitution. Display quoting is not a copy-paste command boundary.

## What Changes

- Use the existing POSIX argument formatter for prerequisite install hints after existing URL credential redaction.
- Preserve typed warning identity, selected resources and required kind/location flags. Keep safe ordinary values readable.

## Capabilities

### Modified Capabilities

- `resource-prerequisites`: literal prerequisite command arguments in remediation hints.

## Impact

Human-readable prerequisite remediation only, under confirmed F152 / #432. No parser/schema/selection change, network or shell execution, credentials, authentication policy or terminal escape overhaul.

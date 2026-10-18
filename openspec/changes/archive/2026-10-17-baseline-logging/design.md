## Decisions

Harvest only observable behavior supported by the current logger,
user-settings loader, and command lifecycle. Existing logger and terminal
session tests prove JSON output, environment overrides, debug mirroring,
fallback, and screen isolation. Configuration tests prove included user
settings and skeleton creation. Add a focused default/override test for the
remaining configuration claims rather than treating prose as evidence.

Retain the full settings table and examples in the public logging reference.
Update source comments to point there, delete `docs/logging.md`, and complete
harvest task 1.7. No production logging code changes.

## Discarded claims

- First-run creation of a v2 consumer configuration contradicts the exact-v4
  contract and is discarded. Root creation and harness selection remain
  governed by consumer configuration and initialization behavior.
- Package layout, the instruction to use `slog`, and where constants live
  are contributor implementation guidance, not observable requirements.
- Example paths and YAML are explanatory instances of the settings
  contract, not separate scenarios.

## Validation

Run the logging and configuration tests freshly, the command lifecycle test,
and strict spec traceability checks. Every new scenario has an executable
test annotation.

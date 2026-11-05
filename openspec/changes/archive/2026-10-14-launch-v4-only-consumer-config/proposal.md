## Why

Public launch should have one supported consumer configuration contract, not multiple decoders, editors, and interactive conversion paths. Complete removal of v1/v2/v3 consumer configuration support makes unsupported input fail predictably and keeps the shipped CLI focused on the explicit v4 source/resource model. Existing installations must convert before upgrading; this is an intentional breaking change, not a deprecation period.

## What Changes

- **BREAKING:** Require the exact string `apiVersion: agent-marketplace/v4` in every existing consumer root: organization, user-personal, repository-committed, and personal-local. Refuse versionless roots, explicit v1/v2/v3, unknown versions, and invalid version values with stable coded diagnostics identifying the offending file and `apiVersion` field.
- Included fragments may omit `apiVersion` and inherit v4 from their validated root; explicit fragment versions must be exactly v4. Preserve source/resource scope, include authority, provenance, and ordered selection decisions.
- Validate the complete configuration needed by an operation before any new config or acquisition-lock publication, harness writes/prunes, or remote acquisition. This does not prohibit ordinary operational logs or change safe recovery of a pre-existing dependency journal.
- **BREAKING:** Remove the shipped `migrate` command, its `--to v2` and `--to v4` paths, CLI startup conversion prompts, TUI migration modal, legacy consumer decoders, and legacy editor fallbacks. Users convert writable roots with an older released migrator before upgrading; consumer organization configuration is converted externally.
- Preserve the runtime requirement model and v4 adapter, already-supported v4 `update --bump`, acquisition-lock v1 reads/v2 writes, historical source-revision hashing, ownership state, and transaction recovery. Marketplace operator configuration, API/wire formats, resource artifacts, source layouts, and harness target formats are not consumer configuration and remain unchanged.
- Port positive selection, policy, safety, and materialization tests to v4 rather than deleting coverage because fixtures use legacy syntax. Retire dedicated legacy-format-success and in-process migration requirements, replacing them with refusal matrices.
- Make successful E2E consumer-command/flag coverage v4-only; retain successful coverage of schema-independent commands without requiring a consumer root. Remove the pre-v4 binary upgrade gate and its CI job, replacing its launch acceptance role with witnessed refusal tests and v4 regressions for its still-applicable lock, scope, collision, and ownership behavior. Keep the released-client gate pinned to checksum-verified v4.1.4 and port its main repository fixture to v4.
- Update existing public documentation and repository guidance to describe v4-only operation and pre-upgrade conversion without advertising migration in the new binary.

## Capabilities

### New Capabilities

- `consumer-config-format`: Exact root and fragment version acceptance, path-specific refusal before new operations, safe pending-transaction handling, and absence of an in-process consumer migration surface.

### Modified Capabilities

- `config-layering`: Replace legacy Marketplace expression/validation with v4 sources/resources and retained acquisition-lock validation, allowlist authority, and provenance.
- `regression-gates`: Preserve functional selection and stable-client interoperability coverage without requiring consumer migration; specify v4-only successful command coverage and the replacement launch/CI gate contract.
- `tui-interactions`: Remove migration from modal intent while retaining guarded save, cancellation, sync-and-quit, and quit behavior.
- `config-mutation`: Remove consumer migration from scoped-publication requirements without weakening link safety, layer ownership, or generation checks.
- `deployed-acquisition-locks`: Record resource-free v4 Marketplace registrations in writable acquisition locks, and refuse unrecorded registrations in read-only dependency modes instead of fetching an unpinned identity.

## Impact

Affected implementation areas include `internal/config`, `internal/pipeline`, consumer paths in `cmd/agent-marketplace`, `internal/tui`, their callers/tests, E2E command coverage and historical-binary fixtures, `Makefile`, CI planning/aggregation, existing site documentation, and repository instructions. No new dependencies are needed. The v4 runtime adapter may be renamed to `ToRuntimeConfig`; removing its requirement model would be a separate redesign and is not part of this change.

Unsupported files remain available for external conversion rather than being silently rewritten or treated as absent. Conversion can be refused by the older migrator when a legacy configuration is unrepresentable; users must resolve that before upgrading. This proposal does not promise seamless execution of a pre-v4 installation by the launch binary. Released v4 clients must continue consuming shared v4 configuration and acquisition locks written by the candidate.

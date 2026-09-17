# Security boundary hardening

## Scope

Implement five narrowly scoped fixes: reject ambiguous typed JSON fields, prevent extension metadata from supplying reserved executable fields, confine automatic gitignore writes, create sensitive shared aggregates privately, and enforce the existing configuration size limit on included files.

Do not change executable lookup policy, Git authentication configuration, resource precedence, lock identities, consent semantics, or partial-compilation pruning. Broader containment, credential-redaction, graph-budget, and conversion-policy changes require separate designs and verification.

## Review structure

Three independent local stacks start from the current main branch. Each layer includes its regression tests and fix. The plan is committed with the first decoding layer. No branches or pull requests are published without approval.

### Stack A: executable configuration

1. `fix/security-decode-aliases`
   - Reject multiple spellings that map to one modeled JSON field before typed decoding, including exact duplicates and aliases differing only in case.
   - Use one schema-aware policy in the existing strict JSON utility. Apply it at resource-payload decoding and native typed JSON/JSONC boundaries, including MCP custom decoders.
   - Preserve a single accepted field alias and consume it as modeled data rather than also retaining it as an extension.
   - Keep arbitrary map keys case-sensitive: environment-variable names, server names, and extension dictionaries are not struct-field aliases.
   - Preserve existing duplicate-key diagnostics and avoid including values in errors.
   - Cover canonical inputs, empty values, both alias orders, nested typed fields, singleton aliases, and case-distinct map keys. Preserve canonical artifact round trips.

2. `fix/security-reserved-extras`
   - Reserved hook execution fields cannot be supplied by extension metadata, even when the corresponding typed value is empty or absent.
   - Apply the protection through shared compilation helpers and every affected hook translator, including native hook-group extras.
   - Retain unrelated extensions and existing coded conflict diagnostics; do not globally discard extension metadata or case-fold arbitrary dictionaries.
   - Cover empty and nonempty typed commands, mixed-case aliases, matcher/type/prompt/timeout fields, supported group fields, and unrelated extension preservation across targets.

### Stack B: filesystem publication

1. `fix/security-gitignore-symlink`
   - Keep the existing best-effort public helper and its fixed ignore-entry behavior.
   - Open the repository directory as an anchored root and open the gitignore leaf without following symlinks. Validate the opened descriptor is a regular file before reading or writing; use nonblocking open to avoid FIFO hangs.
   - Read and append through the same descriptor so replacing the leaf with a symlink cannot redirect a later path-based write.
   - Refuse multiply linked files before mutation to avoid modifying a pre-existing external hard-link alias. Concurrent mutation by an actor with unrestricted same-user filesystem access is not a sandboxing guarantee.
   - Preserve existing bytes and permissions, newline behavior, and idempotency. Skip safely when the local config is absent or the output is unsuitable; log refusals without sensitive bytes.
   - Test external and dangling symlinks, regular and missing files, no-op updates, directories, FIFOs, and hard links using isolated fixtures. Verify external bytes remain unchanged.

2. `fix/security-private-new-aggregates`
   - Default newly created ownership-reconciled shared configuration files to owner-readable/writable permissions.
   - Preserve the mode of existing files and retain the journal, mandatory backup, compare-and-swap, and no-op behavior.
   - Direct compiler MCP artifacts should declare private creation modes; fragment merging must retain that mode. Ordinary resource files retain their existing defaults.
   - Preserve existing destination permissions when updating shared aggregate artifacts through direct materialization.
   - Test fresh creation, restrictive umask behavior, existing private/custom modes, merged MCP fragments, direct conversion, and unchanged-byte no-op behavior. Use dummy values, never real credentials.

### Stack C: configuration input

1. `fix/security-bounded-config-includes`
   - Route disk-backed include reads through the existing bounded configuration reader.
   - Enforce the existing 4 MiB per-file limit consistently for disk input and overlays, retaining `ErrConfigTooLarge` identity.
   - Open nonblocking and require an opened regular file before reading so devices and FIFOs cannot block or produce an endless stream. Retain supported symlinks to ordinary config files and existing organization include policy.
   - Preserve include precedence, canonical-path deduplication, cycle detection, and root/local API-version behavior.
   - Cover small valid input, exact size boundary, oversized roots/includes/overlays, regular-file symlinks, devices/FIFOs, duplicate includes, and cycles.
   - Do not introduce a new aggregate graph-size threshold in this change.

## Verification

Use red-green regression tests for each fix. Assertions must demonstrate both the rejected behavior and a valid control, check stable error/diagnostic identities, and verify unchanged external bytes and absence of temporary residue. Tests must not read personal configuration or contact external services.

Run focused package tests for each layer. For changed hot paths, add a focused benchmark, capture the baseline before the implementation, and compare the same benchmark afterward. Run affected CLI E2E cases for filesystem and include behavior. Run compiler/materializer tests and harness configuration validation when native output changes.

After all layers are reviewed, integrate the stack tips on a local verification branch and run one combined test/build/lint gate. Keep shared E2E-binary gates sequential. Record commands and results in the implementation handoff. Do not modify dependency versions, security controls, or repository Git configuration to make verification pass.

## Completion criteria

- All five behaviors have regression coverage and reviewed fixes.
- Typed inspection and emitted executable fields agree; unmodeled metadata cannot introduce reserved hook behavior.
- Automatic gitignore updates do not follow a leaf symlink or modify a multiply linked file.
- New sensitive aggregates are private; existing permissions and handwritten data remain intact.
- Included config files cannot bypass the existing byte limit or block reads through non-regular files.
- Local branches and commits reflect the planned review boundaries, and no temporary verification artifacts remain in the worktree.

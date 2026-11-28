# Design

## Context

The `target` Cobra command group already emits JSON through the command's output writer. `internal/targetprofile.Parse` validates profile bytes against an exact revision identity, `ValidatePackageDestinations` enforces profile destination allowlists, and `LoadPackage` reads the existing normalized sidecar and payload representation. `internal/place.BuildPlan` is a pure whole-package collision planner; current place publication code also has read paths for destination observations and ownership projections, separate from its commit path.

There is no command for the author-facing `target-import/v1` manifest. The new behavior must bridge that manifest to the existing target-profile package representation without adding a second persisted sidecar schema or entering the consumer install/sync lifecycle.

## Goals / Non-Goals

**Goals:**

- Validate one package from explicit local profile, source, and import files.
- Return the settled JSON report shape and stable, path-specific refusal diagnostics.
- Preview an optional existing destination using place's ownership and collision semantics without changing it.
- Preserve a clear boundary between structural import validity, advisory setup metadata, and runtime behavior.

**Non-Goals:**

- Acquiring or publishing packages, writing a sidecar, installing files, or changing consumer configuration.
- Approval, organization policy loading, host discovery, probing, or executing host/package code.
- Runtime correctness verification, TUI, web UI, or changes to target placement behavior.

## Decisions

### Keep the command under `target` and emit JSON directly

Add the exact `agent-marketplace target dry-run --profile <file> --source <dir> --import <file> [--destination <dir>]` surface to the existing target command group. The command always emits the versioned JSON report; it does not add a separate text mode or an extra output flag. Use the Cobra command's injected output writer, matching the existing JSON command convention. A semantic refusal still emits `status: "refused"` and exits nonzero.

An independent top-level command or TUI flow would split target-profile operations across surfaces and is not part of the settled interface.

### Validate local inputs, then normalize in memory

Read the profile and import manifest only from the supplied paths. Parse the profile through the existing target-profile validation seam and derive its revision from the exact bytes. Parse the manifest as one strict YAML document: require the exact API version and identity fields; reject duplicate keys, aliases, merge keys, unknown fields, malformed values, and ambiguous paths. Match `target_id`, scoped `kind`, and `name` to the profile using the same ASCII and length rules as `nameutil`: target IDs and local kinds start with a lowercase letter and allow lowercase letters, digits, and hyphens up to 64 bytes; package names start with a lowercase letter or digit and allow lowercase letters, digits, `.`, `_`, and `-` up to 128 bytes.

Treat one manifest as one package. Read mapped files beneath the canonical source directory without following symlinks; require selected files to be regular files, and reject special files, path escapes, duplicate source/destination mappings, and unselected package-owned dependencies. Destinations must be below an allowed `into` path or match an exact `support_files` entry. Never expand siblings, follow repository references, substitute content, or run an installer.

Represent selected source files in memory as the existing package file records, retain their bytes, and apply the existing destination validation and binding rules. Normalize identity, file mappings, requirements, and setup metadata to the existing sidecar schema in memory only. Put selected files in `mappings`; put `display_files` in `excluded_files` with reason `display` and other source files in `excluded_files` with reason `not-selected`. Reserve `diagnostics` for problems. No `agent-marketplace.yaml`, payload tree, cache entry, or other file is written. Use `targetprofile.LoadPackage` in parity fixtures to verify that the same normalized fields and bytes are accepted by the existing package reader.

An unmet declared package requirement remains advisory and is reported as such. A dependency that must be inside this package but is absent from its mapped payload is a refusal. Pending `manual_setup` is reported independently. Neither outcome claims that source code works at runtime.

### Reuse place planning only for the optional read-only preview

Without `--destination`, set `destination_check.status` to `not-requested`, with empty `conflicts` and `ownership_changes`; do not resolve a default target root. With `--destination`, require an existing canonical destination admitted by the profile's root rules. Read only the exact paths and existing ownership evidence needed for the preview, then use the same place plan semantics, including whole-package conflicts and refusal of an unowned file even when its bytes match. Report conflicts and predicted ownership transitions without applying them.

Reuse the pure `internal/place.BuildPlan` decision logic and existing place observation/ownership rules. `destination_check.ownership_changes` contains only transitions predicted by that plan; `claim` identifies a newly claimed path, while an unchanged already-owned path is omitted. If any mapped path refuses the whole package, return an empty change list, never a partial claim list. Do not enter place admission, publication, recovery, or commit paths to perform a preview. Missing or unusable state is never created or repaired; it cannot be treated as proof that an existing file is owned.

An independent collision implementation was rejected because it could diverge from place's path normalization and ownership decisions. Calling the existing publication path was rejected because it can stage and mutate state and is not a read-only preview.

### Keep report shape and refusal meaning stable

Use the report keys shown below: `schema_version`, `status`, `profile`, `package`, `mappings`, `excluded_files`, `requirements`, `manual_setup`, `destination_check`, and `diagnostics`. `mappings` identifies selected source and native destination paths. `excluded_files` records each excluded source path and a distinct reason, including `display` and `not-selected`. `destination_check` includes `ownership_changes`, each with a destination path and predicted action. `diagnostics` is reserved for problems and includes stable codes and relevant paths on refusal; exclusions are not diagnostics.

The JSON example uses both explicit report fields and keeps advisory requirements and setup separate from problems.

### Example local import manifest

```yaml
apiVersion: agent-marketplace.io/target-import/v1
target_id: sample-app
kind: sample-app/mod
name: sample-links
mappings:
  - source_path: mods/sample-links/main.js
    destination_path: mods/sample-links/main.js
  - source_path: mods/sample-links/mod.json
    destination_path: mods/sample-links/mod.json
display_files: [README.md]
metadata:
  requires: ["sample-app/mod:sample-tabs"]
  manual_setup: [Bind the test key in Sample app settings.]
```

### Example JSON report

```json
{
  "schema_version": 1, "status": "valid",
  "profile": {"id": "sample-app", "revision": "sha256:<64-hex>"},
  "package": {"target_id": "sample-app", "kind": "sample-app/mod", "name": "sample-links"},
  "mappings": [
    {"source_path": "mods/sample-links/main.js", "destination_path": "mods/sample-links/main.js"},
    {"source_path": "mods/sample-links/mod.json", "destination_path": "mods/sample-links/mod.json"}
  ],
  "excluded_files": [
    {"source_path": "README.md", "reason": "display"},
    {"source_path": "notes/idea.md", "reason": "not-selected"}
  ],
  "requirements": [{"ref": "sample-app/mod:sample-tabs", "status": "unmet"}],
  "manual_setup": [{"status": "pending", "text": "Bind the test key in Sample app settings."}],
  "destination_check": {"status": "not-requested", "conflicts": [], "ownership_changes": []},
  "diagnostics": []
}
```

### Example supplied-destination excerpt

```json
{
  "destination_check": {
    "status": "checked",
    "conflicts": [],
    "ownership_changes": [
      {"destination_path": "mods/sample-links/main.js", "action": "claim"},
      {"destination_path": "mods/sample-links/mod.json", "action": "claim"}
    ]
  }
}
```

### Keep the command independent of consumer and host state

Do not load effective consumer configuration, acquire a source or acquisition lock, fetch a lock, probe a host, request approval, create ownership state, or run sync/install. The command needs only its explicit local inputs. This also applies to refusals: validation must not leave target, config, lock, approval, ownership, cache, registry, or other application state changed or created.

## Risks / Trade-offs

- [Risk] A destination preview can become stale before a later placement → Mitigation: describe it only as an advisory snapshot; it grants no ownership, approval, reservation, or installation success.
- [Risk] Static checks cannot prove behavior of arbitrary package code → Mitigation: report structural validity and manual setup only; never claim runtime success or execute package code.
- [Risk] Import and normalized-package handling could diverge → Mitigation: share existing profile/destination/binding rules and add parity scenarios against the normalized package reader.

## Migration Plan

None. The change adds a local command and writes no persistent format or state. Rollback consists of removing the command and its report handling.

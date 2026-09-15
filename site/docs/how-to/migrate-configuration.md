# How to migrate configuration

This guide shows how to remediate structurally invalid `agent-marketplace` configuration.

## Prerequisites

- `agent-marketplace` on your `PATH`.
- A v2 or v3 file with legacy structural issues, or a v1 file that you have already converted into a valid v2 root.

## Important: v1 roots are rejected

`agent-marketplace migrate` rejects v1 configuration roots. v1 files have no `apiVersion` and must first be converted into valid v2 by hand before further remediation. `migrate` does not rewrite v1 into v2 for you.

## 1. Identify the problem

Run `status` to see the failure:

```bash
agent-marketplace status
```

If a root is rejected because it is v1, the error mentions the missing `apiVersion`.

## 2. Convert v1 to v2 by hand (if necessary)

A v1 file must become a valid v2 root before `migrate` can help. Add `apiVersion: agent-marketplace/v2`, move legacy structural fields to their v2 shapes, and remove obsolete fields such as `identity` and `team_profiles`.

## 3. Run the migration

For a repository and its user config with a target harness set:

```bash
agent-marketplace migrate --harnesses claude,factory
```

Then sync:

```bash
agent-marketplace sync
```

## 4. Verify the output

Inspect `.agents/config.yaml` and confirm:

```yaml
apiVersion: agent-marketplace/v2
harnesses:
  - claude
  - factory
registries:
  - url: https://github.com/example-org/infra-skills.git
    ref: main
    skills:
      include:
        - vm-lifecycle
```

## Boundaries

- `migrate` preserves valid v2 and v3 files and rewrites only structurally invalid content.
- It does not add `devin` or `codex` implicitly.
- It does not rewrite `identity` or `team_profiles` fields; those files are refused.
- v1 roots are rejected and must be converted to valid v2 first.

## Troubleshooting

- **Migration refuses a file**: The file may be v1 or contain legacy `identity` or `team_profiles` fields. Convert v1 by hand or remove the legacy fields before migrating.
- **Target harnesses are missing after migration**: Pass `--harnesses` explicitly or set them in the configuration afterward.

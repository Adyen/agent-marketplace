# How to use personal repository overrides

This guide shows how to keep project-wide configuration committed while making checkout-only choices in a gitignored local file.

## Prerequisites

- A repository with `.agents/config.yaml`.
- `agent-marketplace` on your `PATH`.

## 1. Create a personal override file

Create `.agents/config.local.yaml` next to the committed configuration:

```yaml
skills:
  include:
    - my-personal-skill
  exclude:
    - some-team-skill
```

Only include skills that do not need to be shared.

## 2. Sync

```bash
agent-marketplace sync
```

The CLI adds `.agents/config.local.yaml` and `.agents/lock.local.yaml` to `.gitignore` automatically when the personal-local layer exists.

## 3. Verify the override

```bash
agent-marketplace status
```

The **LOCALLY EXCLUDED** section shows checkout-only exclusions. The **SYNCED RESOURCES** section shows the extra skill.

## Boundaries

- Local excludes apply only to non-required, repo-scoped resources.
- Required resources and machine-scoped resources cannot be suppressed from a repository-local file.
- Local overrides cannot declare `allowed_marketplaces` or `allowed_registries`.

## Troubleshooting

- **`some-team-skill` still appears**: It may be `required` or machine-scoped. Check its source under **SYNCED RESOURCES**.
- **`.agents/config.local.yaml` is tracked**: Remove it from the index and add `.agents/*.local.*` to `.gitignore`.

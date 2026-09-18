# How to use personal repository overrides

Keep project-wide configuration committed while making checkout-only choices in a gitignored local file.

## Prerequisites

- A repository with `.agents/config.yaml`.
- `agent-marketplace` on your `PATH`.

## 1. Create a personal override file

Suppose the committed configuration is:

```yaml
# .agents/config.yaml
apiVersion: agent-marketplace/v2
harnesses: [claude]
registries:
  - url: https://github.com/example-org/agent-resources.git
    ref: v1.4.0
    skills:
      include:
        - code-review
        - some-team-skill
```

Create `.agents/config.local.yaml` next to it. Repeat the registry `url` (and `ref`) so the CLI knows which provider your additions and exclusions apply to:

```yaml
# .agents/config.local.yaml
registries:
  - url: https://github.com/example-org/agent-resources.git
    ref: v1.4.0
    skills:
      include:
        - my-personal-skill
      exclude:
        - some-team-skill
```

The local layer merges above the committed one: `my-personal-skill` is added to the selection and `some-team-skill` is excluded for this checkout only. `harnesses` is not allowed in the local file (`apiVersion` is optional for normal loading, though required if you run `agent-marketplace migrate`). Only include resources that do not need to be shared.

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

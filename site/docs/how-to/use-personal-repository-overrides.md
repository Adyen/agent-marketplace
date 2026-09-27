# How to use personal repository overrides

Keep the team's configuration committed while you add or hide resources in your own checkout only. Your choices go in `.agents/config.local.yaml`, a file Git ignores.

## Prerequisites

- A repository with `.agents/config.yaml`.
- `agent-marketplace` on your `PATH`.

## 1. Create a personal override file

Suppose the committed configuration is:

```yaml
# .agents/config.yaml
apiVersion: agent-marketplace/v4
harnesses: [claude]
sources:
  shared:
    git: https://github.com/example-org/agent-resources.git
    ref: v1.4.0
resources:
  shared:
    include:
      - skill/my-team-skill
      - skill/some-team-skill
```

Create `.agents/config.local.yaml` next to it. Start it with `apiVersion: agent-marketplace/v4`, and repeat the source declaration so the CLI knows which registry your additions and exclusions refer to:

```yaml
# .agents/config.local.yaml
apiVersion: agent-marketplace/v4
sources:
  shared:
    git: https://github.com/example-org/agent-resources.git
    ref: v1.4.0
resources:
  shared:
    include:
      - skill/my-personal-skill
    exclude:
      - skill/some-team-skill
```

The local file is applied on top of the committed one. In this checkout, `my-personal-skill` is added and `some-team-skill` is hidden. Without the `apiVersion` line the CLI rejects `sources` and `resources` in this file, and without the repeated source it reports `v4.undefined-source`. The local file cannot contain `harnesses`. Put only resources here that the team does not need.

You can also let the CLI write this file: `agent-marketplace install <name> --scope local` and `agent-marketplace remove <name> --scope local` edit `.agents/config.local.yaml` for you.

## 2. Sync

```bash
agent-marketplace sync
```

When `.agents/config.local.yaml` exists, the CLI adds `.agents/config.local.yaml` and `.agents/lock.local.yaml` to the repository's `.gitignore` if they are not listed yet. Sync writes `.agents/lock.local.yaml` for the Git registries in your local file.

## 3. Verify the override

```bash
agent-marketplace status
```

In `SYNCED RESOURCES`, `my-personal-skill` appears with `REPO*` in the `SOURCE` column, which marks the personal-local layer, and `some-team-skill` is no longer listed. The `LOCAL REGISTRIES` section lists the registries your local file declares. `status` does not list v4 local exclusions in its `LOCALLY EXCLUDED` section, so check that the resource is missing from `SYNCED RESOURCES` instead.

## Boundaries

- Local exclusions apply only to resources that come from repository configuration and are not required.
- A local file cannot hide required resources or resources from your user or organization configuration.
- A local file cannot declare `harnesses`, `allowed_marketplaces`, `allowed_registries`, `backup`, `logging`, or `telemetry`.

## Troubleshooting

- If `some-team-skill` still appears, it may be required, or it may come from your user or organization configuration. Check its `SOURCE` column under `SYNCED RESOURCES`.
- If `.agents/config.local.yaml` is already tracked by Git, remove it from the index with `git rm --cached .agents/config.local.yaml` and make sure `.gitignore` lists it.

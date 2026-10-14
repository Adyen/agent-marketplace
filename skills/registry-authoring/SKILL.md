---
name: registry-authoring
description: >
  How to create and maintain a registry that agent-marketplace can
  discover, ingest, and sync into harness-native layouts. Use when creating
  a registry repo, adding skills, commands, subagents, hooks, MCP servers, or
  collections to one, writing registry CI validation, releasing a registry,
  or troubleshooting resources that are missing or malformed.
metadata:
  tags:
    - registry
    - skills
    - authoring
    - agent-marketplace
---

# Registry Authoring

A registry is a plain Git repository (or local directory) whose resources are
discovered from the native layout of one **source harness**. There is no
manifest: the CLI reads the layout's well-known paths. The authoritative
references are
[registry layout](https://github.com/Adyen/agent-marketplace/blob/main/site/docs/reference/registry-layout.md),
[resource metadata](https://github.com/Adyen/agent-marketplace/blob/main/site/docs/reference/resource-metadata.md),
and [collections](https://github.com/Adyen/agent-marketplace/blob/main/site/docs/reference/collections.md).

## Minimal registry (Claude layout)

```
my-registry/
├── skills/
│   ├── deploy-conventions/
│   │   ├── SKILL.md
│   │   └── references/checklist.md   ← ships with the skill
│   └── incident-response/
│       └── SKILL.md
├── commands/
│   └── release.md
├── collections/
│   └── platform-onboarding/
│       └── COLLECTION.yaml
├── CODEOWNERS
└── README.md
```

A consumer selects from it with a v4 config:

```yaml
# .agents/config.yaml
apiVersion: agent-marketplace/v4
sources:
  platform:
    git: git@github.com:example-org/my-registry.git
    ref: v1.4.0
    source_harness: claude     # state the layout instead of relying on detection
resources:
  platform:
    include:
      - skill/deploy-conventions
      - command/release
      - collection/platform-onboarding
```

Consumers can also select by `tags: [...]` or take everything with `all: true`.
Set the source `path:` when resources live under a subdirectory: `path: resources`
discovers `resources/skills/...`, `resources/commands/...`, and so on.

## Layout by source harness

Paths are relative to the registry root, or to the source's `path`.

| Harness | Skill | Command | Subagent | Hook | MCP server |
|---|---|---|---|---|---|
| `claude` | `skills/<name>/SKILL.md` | `commands/<name>.md` | `agents/<name>.md` | `hooks/hooks.json` | `.mcp.json` at the bundle root |
| `factory` | `skills/<name>/SKILL.md` | `commands/<name>.md` | `droids/<name>.md` | `hooks/hooks.json` | `mcp.json` |
| `cursor` | `skills/<name>/SKILL.md` | `commands/<name>.md` | `agents/<name>.md` | `hooks/hooks.json` | `mcp.json` |
| `windsurf` | `skills/<name>/SKILL.md` | `workflows/<name>.md` | not supported | `hooks.json` | `mcp_config.json` |
| `gemini` | `.gemini/skills/<name>/SKILL.md` or `.agents/skills/…` | `commands/<name>.toml` | `.gemini/agents/<name>.md` | `.gemini/settings.json#hooks` | `.gemini/settings.json#mcpServers` |
| `devin` | `skills/<name>/SKILL.md` | skill dir + `x-agent-marketplace-kind: command` | `agents/<name>.md` | `hooks.v1.json` | `mcp_config.json` |
| `codex` | `.agents/skills/<name>/SKILL.md` | skill dir + `x-agent-marketplace-kind: command` | `.codex/agents/<name>.toml` | `.codex/hooks.json` | `.codex/config.toml#mcp_servers` |

There are no generic `subagents/` or `mcp/` roots: a Claude subagent ships as
`agents/<name>.md`, and Claude MCP servers ship in a root `.mcp.json`. Files in
directories the layout does not read are silently undiscovered. Workspace forms
(`.claude/`, `.cursor/`, `.factory/`, `.windsurf/`, `.devin/` prefixing the same
paths) are also read.

### Autodetection

Without `source_harness`, the CLI detects the layout from markers: `.devin/`,
then plugin manifests (`.claude-plugin/plugin.json`, `.factory-plugin/`,
`.cursor-plugin/`, `gemini-extension.json`), then `.codex/`, then Claude-shaped
roots, and so on. Root-level Cursor, Windsurf, Factory, and Gemini layouts share
folder names with Claude, and a root-level Devin layout (no `.devin/`) is never
detected. Tell consumers to set `source_harness` for those registries. An
undetected registry falls back to Claude with `catalog.autodetect.fallback`.

### Root shapes

A registry root (or the `path` target) may be one bundle in a layout above, a
single `SKILL.md` (which must declare `name`), a folder of skill folders, or a
pure container of bundles. A container's subfolders are searched for bundles,
skipping `.git`, `.github`, `node_modules`, `.cache`, `test`, `tests`,
`__tests__`, harness folders, and resource folders. A root that has its own
resources and also sub-bundles is read as the root only, with
`catalog.discover.sub-bundles-ignored`; move root resources into a subfolder
such as `common/`.

Everything inside a skill folder ships with the skill; a nested `SKILL.md` is a
support file, not a separate skill. Keep skills one level under `skills/`.

## `SKILL.md` frontmatter

Follow the [Agent Skills specification](https://agentskills.io/specification):

```markdown
---
name: vm-lifecycle
description: >
  Create, update, and decommission VMs. Use when working with OpenStack VMs,
  Puppet node definitions, or DNS records for a host.
license: Apache-2.0
metadata:
  tags: [infra, openstack, puppet]
  requires:
    - mcp:service-catalog
    - name: incident-response
      kind: skill
      registry: git@github.com:example-org/operations-skills.git
      path: skills
---
```

| Field | Notes |
|---|---|
| `name` | Lowercase letters, digits, hyphens; starts with a letter. Wins over the folder name; the folder name is the fallback, except for a root-level `SKILL.md`. Keep them equal. |
| `description` | Required by the spec. Write it as a when-to-use trigger naming symptoms, tools, and error conditions. |
| `license` | Optional SPDX identifier. Hosted Marketplaces use it for license policy. |
| `x-agent-marketplace-kind` | `command` marks a skill-shaped command in Devin and Codex layouts only. |
| `metadata.tags` | Strings used by consumer `tags:` and `install --tag`. Matching is exact and case-sensitive. |
| `metadata.requires` | Advisory prerequisites. |

Invalid YAML or an invalid name makes the skill malformed (for example
`ingest.claude.load-skill-failed`): `list` shows `malformed: <reason>`, sync
counts it under `Malformed`, and it is not installed. Harness-specific fields
such as `allowed-tools` and `model` are translated per target; other unknown
fields pass through.

Commands and subagents written as Markdown with frontmatter read `metadata.tags`
and `metadata.requires` from the same place. A Claude command's name comes from
its file name.

### Tags

Tags group resources for bulk selection. Use a small set (3–6) of lowercase
nouns: one domain (`infra`, `security`, `platform`) plus technologies (`k8s`,
`puppet`). A tag that repeats the resource name selects nothing new.

### Prerequisites

`requires` items are `"<kind>:<name>"` strings (a bare name means `skill`) or
maps with `name` and optional `kind`, `registry`, `path`. Kinds are `skill`,
`command`, `subagent`, `hook`, `mcp`; `collection` is not accepted. A
prerequisite is met by any selected resource of that kind and name, from any
source; `registry` and `path` only improve the suggested install command.
Unmet prerequisites warn and never install anything. Items with an unknown kind
or no `name` are skipped with a log warning.

## Collections

A collection bundles resources for one-line selection
(`collection/<name>` in a consumer's `include`). It lives at
`collections/<name>/COLLECTION.yaml`; flat `collections/<name>.yaml` files are
ignored, and consumers match on the `name` field, not the folder.

```yaml
name: platform-onboarding
description: Common starter resources for platform teams
resources:
  - kind: skill
    name: deploy-conventions
  - kind: command
    name: release
  - kind: mcp
    name: service-catalog
    registry: https://github.com/example-org/shared-mcp.git   # must already be a consumer source
includes:
  - name: security-baseline
```

- Member `kind` defaults to `skill`; `collection` is not a member kind (use
  `includes` to nest). The legacy `skills:` list still works; write `resources:`.
- A member without `registry` comes from the collection's own source, ref, and
  path. A collection cannot add a registry to the consumer's config; a member
  from another registry fails unless the consumer already declares it.
- `includes` may name a collection from any configured registry. Shared nested
  collections are fine; including an ancestor is a cycle.
- A missing member warns during `sync` and fails `sync --strict`; `list` does
  not check members.
- The collection itself writes no files; removing one member records an
  exclude and keeps the collection selected.

## Writing the body

The body is what the agent reads after the description fires. Put the ordered
steps first, then decision rules, reference tables, and gotchas. Name exact
files, fields, and commands:

```markdown
## Step 1: Network configuration (network-configurations repo)
Add the VM's IP allocation in `networks/<dc>/hosts.yaml` under the
datacenter section, with `hostname`, `ip`, and `netmask`.
```

Reference support files from the body (`See references/repo-map.md`) so the
agent knows to open them.

## Check locally

```bash
agent-marketplace list --from .            # every resource should be `available`
```

A clean conversion reports `Diagnostics 0`. Look up `ingest.*` codes in the
[diagnostics reference](https://github.com/Adyen/agent-marketplace/blob/main/site/docs/reference/diagnostics.md).

## CI validation

Sync a throwaway consumer against the checkout with a pinned CLI
(`v4.0.9` or newer) and every harness your consumers target:

```bash
set -euo pipefail                       # run under bash
REGISTRY_PATH=$PWD
CONSUMER=$(mktemp -d)
mkdir -p "$CONSUMER/.agents"
cat > "$CONSUMER/.agents/config.yaml" << EOF
apiVersion: agent-marketplace/v4
harnesses: [claude, factory, cursor, windsurf, gemini, devin, codex]
sources:
  checkout:
    directory: ${REGISTRY_PATH}
resources:
  checkout:
    all: true
    required: true
EOF
cd "$CONSUMER"
agent-marketplace sync --strict -v | tee sync.log
grep -q 'Malformed 0  Errors' sync.log   # --strict alone does not fail on malformed resources under all: true
agent-marketplace sync --check
for kind in skill command subagent hook mcp collection; do agent-marketplace list --kind "$kind"; done
```

Compare the `list` output with a checked-in expectation so a resource in an
unread directory fails CI. The
[CI how-to](https://github.com/Adyen/agent-marketplace/blob/main/site/docs/how-to/author/validate-a-registry-in-ci.md)
explains each step and the download command for a pinned release.

## Access control

`CODEOWNERS` plus branch protection is the trust boundary: whoever can merge
into the registry ships resources to every consumer. Require review for
`skills/` and stricter owners for sensitive paths.

## Versioning and releases

- Tag releases with semantic versions and keep a `CHANGELOG.md`. Renaming or
  removing a resource is a major release.
- Consumers pinned to a tag stay on it until they edit `ref:` and run `sync`.
  Consumers on a branch stay on their locked commit until `update`.
- `update --bump` rewrites refs only in legacy v2/v3 `registries:` configs, and
  only to tags that differ in the final number. v4 consumers edit the ref.
- Document the consumer v4 snippet and contribution rules in the registry
  README.

## Common mistakes

| Mistake | Symptom | Fix |
|---|---|---|
| Resource in a directory the layout does not read (`subagents/`, `mcp/`) | Missing from `list` | Use the source harness's native path |
| Root-level Cursor/Windsurf/Factory/Devin layout without `source_harness` | Read as Claude; resources missing | Consumers set `source_harness` |
| Skill nested inside another skill | Inner skill not discovered | Keep skills one level under `skills/` |
| `name` with capitals, spaces, or underscores | Skill reported malformed | Use kebab-case matching the folder |
| Root resources plus sub-bundles | `catalog.discover.sub-bundles-ignored` | Move root resources into a subfolder |
| Collection as `collections/<name>.yaml` | Collection not found | Use `collections/<name>/COLLECTION.yaml` |
| Collection member from an unconfigured registry | Expansion error | Tell consumers to add that source first |
| Same kind and name in two registries | Consumer warns and picks one winner | Rename, or consumers narrow selections |
| Vague description | Agents never activate the skill | Name triggers, tools, and error text |
| No CODEOWNERS | Unreviewed changes reach consumers | Add CODEOWNERS and branch protection |

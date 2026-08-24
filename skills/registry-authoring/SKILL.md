---
name: registry-authoring
description: >
  How to create and maintain a skill registry that agent-marketplace can
  discover, ingest, and sync into harness-native layouts. Use when creating
  a new registry repo (like infra-skills), adding skills to an existing
  registry, writing CI validation, or troubleshooting discovery failures.
metadata:
  tags:
    - registry
    - skills
    - authoring
    - agent-marketplace
---

# Registry Authoring

A registry is a plain Git repository whose resources (skills, commands,
subagents, hooks, MCP servers) are auto-discovered by agent-marketplace
from their directory layout and `SKILL.md` frontmatter. No manifest file
is required — the tool walks the tree looking for well-known files.

This skill covers the conventions, pitfalls, and operational patterns
that make a registry reliable and pleasant to consume.

## Minimal Registry

```
my-skills/
├── skills/
│   ├── deploy-conventions/
│   │   └── SKILL.md
│   └── incident-response/
│       └── SKILL.md
├── CODEOWNERS
└── README.md
```

Before syncing, configure the target harness set in the project config or,
when shared across projects, in `~/.agents/config.yaml` or
`/etc/agent-marketplace/config.yaml`:

```yaml
apiVersion: agent-marketplace/v2
harnesses: [factory]
registries: []
```

The committed repository config may declare its own top-level `harnesses`;
when omitted, it inherits the user or organization target set.

Consuming repos point at it:

```yaml
# .agents/config.yaml
apiVersion: agent-marketplace/v2
registries:
  - url: git@gitlab.example.com:example-org/my-skills.git
    ref: main
    path: skills
    skills:
      include: [deploy-conventions]
```

## Directory Structure

### Path prefix

Use a top-level directory (e.g. `skills/`, `commands/`, `hooks/`) to
group resources by kind. Set `path: skills` in the consumer config so
agent-marketplace scans only the relevant subtree.

```
team-skills/
├── skills/           ← path: skills
│   └── vm-lifecycle/
│       └── SKILL.md
├── commands/         ← path: commands
│   └── deploy.md
├── hooks/
│   └── pre-push.json
└── mcp/
    └── service-catalog.json
```

You can also list multiple registries pointing at the same repo with
different `path` values, or a single registry with `path: .` to scan
everything.

### Resource directory layout

Each resource is a directory whose identity file lives at a known
location. Skill directories **must** contain `SKILL.md`:

```
vm-lifecycle/
├── SKILL.md           ← required; identity + instructions
├── references/
│   └── repo-map.md    ← auxiliary content (not a separate skill)
└── scripts/
    └── validate.sh    ← supporting asset
```

Non-skill resources use their own harness-native layouts:
- **Commands** — standalone `.md` files or `commands/<name>/<name>.md`
- **Hooks** — `hooks.json` files
- **MCP servers** — `mcp.json` files
- **Subagents** — droid YAML/JSON files

### What discovery considers a skill

`agent-marketplace` walks the configured `path` and:

1. Any directory containing `SKILL.md` is a skill. The directory name
   is the default skill name (overridden by frontmatter `name:`).
2. Standalone `.md` files with valid frontmatter containing a `name:`
   field are also skills (useful for commands).
3. Nested `SKILL.md` files inside an already-discovered skill directory
   are **not** separate skills — they are supplementary assets.
4. Directories named `node_modules`, `.git`, `.cache`, `test`,
   `tests`, `__tests__` are skipped.
5. Hidden directories (dot-prefix) are skipped unless the path escapes
   upward (e.g. `../other-registry`).

**Do not** nest skill directories inside other skill directories and
expect them to be independently discovered. Use a flat or shallow
two-level structure instead.

## SKILL.md Format

Follow the [agentskills.io specification](https://agentskills.io/specification).
Every skill directory must contain a `SKILL.md` with YAML frontmatter:

```markdown
---
name: vm-lifecycle
description: >
  How to create, update, and decommission VMs across infrastructure.
  Use when working with OpenStack VMs, Puppet node definitions, or DNS
  records.
metadata:
  version: "1.2"
  tags:
    - openstack
    - puppet
    - network
---

# VM Lifecycle

## When to use
Use this skill when creating, modifying, or decommissioning virtual machines.

## Step-by-step: Creating a new VM
...
```

### Frontmatter rules

| Field | Required | Notes |
|-------|----------|-------|
| `name` | yes | Must be unique across the same registry. Kebab-case. |
| `description` | yes | Multi-line OK with `>`. Shown in `list` and TUI. |
| `metadata.version` | optional | String (quote it). Not semver-enforced. |
| `metadata.tags` | optional | List of strings. Used by `install --tag` and `tags:` in config. |
| `metadata.requires` | optional | List of prerequisites. Use `"mcp:name"` shorthand or a map with `name`, `kind`, and optional `registry`/`path`. Advisory only. |

The frontmatter must be delimited by `---` on its own line. The parser
extracts content between the first and second `---` markers as YAML.
Malformed frontmatter causes the skill to surface as "malformed" in
`list` and `status` output, and blocks `install`.

### Naming conventions

- Use **kebab-case**: `vm-lifecycle`, `enc-debugging`, `k8s-app-onboarding`.
- Name should describe the workflow or domain, not the tool: prefer
  `incident-response` over `pagerduty-workflow`.
- The directory name and frontmatter `name:` should match. When they
  differ, frontmatter wins for deduplication but directory name is used
  for `install` and `sync` unless frontmatter is parsed successfully.
- Avoid names that collide across registries; same-name skills from
  different registries cause sync errors in the consumer.

### Description best practices

The description is the primary signal for agents deciding whether to
activate a skill. Write it as a **when-to-use** trigger:

```yaml
description: >
  Debug the Puppet External Node Classifier when a node's classification
  is missing or wrong. Use when ENC errors appear in Puppet runs, or when
  a VM is assigned to the wrong environment/pool.
```

**Good**: Mentions specific symptoms, tool names, and error conditions.
**Bad**: "A skill about the ENC" — too vague for an agent to match
against a user request.

### Tag strategy

Tags enable `install --tag <tag>` and `tags: [...]` in consumer config.
Treat tags as **broad categories** that group related skills for
bulk selection:

```yaml
metadata:
  tags:
    - infra          # domain: infrastructure operations
    - openstack      # technology: OpenStack
    - lifecycle      # pattern: create/update/delete workflows
    - puppet         # technology: Puppet
```

Guidelines:

- Prefer lowercase, singular nouns: `network` not `Networks`.
- Include one **domain** tag (infra, security, platform) and one or
  more **technology** tags (k8s, openstack, puppet).
- Don't duplicate the skill name as a tag — it's redundant.
- Keep the set small (3–6 tags). Every tag is a potential `install
  --tag` filter, so each one should pull in a meaningful cohort.

### Declaring prerequisites

Declare resources that a skill needs under `metadata.requires`:

```yaml
metadata:
  requires:
    - mcp:service-catalog
    - name: incident-response
      kind: skill
      registry: git@gitlab.example.com:team/operations-skills.git
      path: skills
```

Use `"<kind>:<name>"` shorthand, or a map with required `name` and
optional `kind`, `registry`, and `path`. A bare `"name"` or omitted
`kind` defaults to `skill`. Supported kinds are `skill`, `command`,
`subagent`, `hook`, and `mcp`; `collection` is not supported.
Malformed entries are ignored and produce a structured log warning.

Requirements are advisory. They produce CLI diagnostics when the
matching kind and name are not selected, but they never install a
resource or override an exclude. Use `registry` and `path` only to
improve the suggested install command; do not rely on
`metadata.requires` for enforcement.

## Skill Body Content

The body after frontmatter is the actual instruction set that agents
read. Structure it for **machine consumption first, human reference
second**:

1. **When to use** — triggers that help the agent decide to activate.
2. **Decision trees** — conditional logic based on context.
3. **Step-by-step procedures** — ordered actions spanning repos/tools.
4. **Reference tables** — mappings, constants, repo relationships.
5. **Gotchas** — common mistakes, ordering constraints, failure modes.

Keep instructions **precise and actionable**. Avoid prose that requires
interpretation:

```markdown
## Step 1: Network configuration (network-configurations repo)
Add the VM's IP allocation in `networks/<dc>/hosts.yaml` under the
correct datacenter section. The entry must include hostname, ip, and
netmask fields.
```

Not:

```markdown
## Step 1
First you need to configure the network. Go to the network repo and
add an entry for the new VM. Make sure it's in the right place.
```

### Supporting assets

Place reference documents, scripts, and templates alongside `SKILL.md`
inside the skill directory. They are **not** independently discovered
(only `SKILL.md` defines a skill), but agents may read them when the
skill is active:

```
network-change-workflow/
├── SKILL.md
├── references/
│   ├── firewall-repo-map.md
│   └── change-template.yaml
└── scripts/
    └── validate-change.sh
```

Reference these assets from the skill body so the agent knows they
exist:

```markdown
## Reference
See `references/firewall-repo-map.md` for the repository layout and
`scripts/validate-change.sh` for pre-merge validation.
```

## Multi-Resource Registries

A registry can contain more than skills. The canonical agent-marketplace v2
schema supports commands, subagents, hooks, and MCP servers from the same
repository, organized by kind:

```
team-resources/
├── skills/
│   ├── vm-lifecycle/
│   │   └── SKILL.md
│   └── enc-debugging/
│       └── SKILL.md
├── commands/
│   ├── deploy-service/
│   │   └── deploy-service.md
│   └── rollback.md
├── hooks/
│   └── pre-tool-audit/
│       └── hooks.json
├── mcp/
│   └── service-catalog/
│       └── mcp.json
└── subagents/
    └── incident-responder.yaml
```

Consumers select resources by kind in a v2 config:

```yaml
apiVersion: agent-marketplace/v2
registries:
  - url: git@gitlab.example.com:team/team-resources.git
    ref: main
    path: skills          # or path: . to scan everything
    skills:
      include:
        - vm-lifecycle
    commands:
      include:
        - deploy-service
    hooks:
      include:
        - pre-tool-audit
    mcp:
      include:
        - service-catalog
    subagents:
      include:
        - incident-responder
```

## CODEOWNERS and Access Control

Registry repos should use `CODEOWNERS` to control who can add or modify
skills. This is the primary security boundary — GitLab merge
protections enforce it.

```
# CODEOWNERS
# All skills require infra-team review
/skills/           @infra-team

# Security skills require security-team review
/skills/security-* @security-team
```

There is no separate trust model. If a user can merge into the registry
repo (per CODEOWNERS + branch protection), their skill is trusted.

## CI Validation

Add a CI job that validates every `SKILL.md` on merge requests:

```yaml
# .gitlab-ci.yml
validate-skills:
  script:
    - |
      for f in $(find skills -name SKILL.md); do
        if ! head -1 "$f" | grep -q '^---'; then
          echo "ERROR: $f missing frontmatter opening ---"
          exit 1
        fi
        if ! grep -q '^name:' "$f"; then
          echo "ERROR: $f missing name field"
          exit 1
        fi
        if ! grep -q '^description:' "$f"; then
          echo "ERROR: $f missing description field"
          exit 1
        fi
      done
```

This catches:
- Missing or malformed frontmatter delimiters
- Missing required `name` or `description` fields (does not validate general YAML syntax — use yq or python-yaml for that)

For stricter validation, check against the agentskills.io JSON schema
if one is published.

## README for the Registry

Every registry repo should have a `README.md` that:

1. Lists available skills with one-line descriptions (auto-generated or
   manual).
2. Shows the `.agents/config.yaml` snippet for consuming the registry.
3. Documents contributing instructions (directory structure, frontmatter
   requirements, review process).

Example from infra-skills:

```markdown
## Integrating with agent-marketplace

```yaml
apiVersion: agent-marketplace/v2
registries:
  - url: git@gitlab.example.com:example-team/infra-skills.git
    ref: main
    path: skills
skills:
  include:
    - vm-lifecycle
    - ip-and-dns-management
```

Run the sync and commit the config:

\`\`\`bash
agent-marketplace sync
git add .agents/config.yaml .gitignore
\`\`\`
```

## Versioning and Refs

- Pin consumers to a **branch** (`main`) for latest, or a **tag**
  (`v1.2.0`) for stability.
- Use `metadata.version` in frontmatter to track skill-level changes
  independently of the repo tag.
- `agent-marketplace update --bump` checks for newer refs when
  consumers pin to a tag.
- Breaking changes to a skill's frontmatter name or tag set should be
  called out in the registry's changelog or release notes.

## Common Mistakes

| Mistake | Symptom | Fix |
|---------|---------|-----|
| Nesting skills inside skills | Inner skill not discovered | Keep skills at the same depth under `path` |
| Missing `---` delimiters | Skill shows as "malformed" | Add opening and closing `---` lines |
| `name:` with spaces or capitals | Install by name fails, inconsistent display | Use kebab-case: `my-skill` |
| Same `name:` in two registries | Sync fails with collision error | Rename one, or narrow per-registry selections |
| Description too vague | Agents don't activate the skill when they should | Include when-to-use triggers and specific error conditions |
| Tag duplication with name | `install --tag vm-lifecycle` is redundant | Tags should group; names already identify |
| No CODEOWNERS | Anyone can merge skill changes | Add CODEOWNERS to enforce review |
| Forgetting `path:` in config | Registry root scanned, may find irrelevant dirs | Set `path: skills` (or appropriate subdirectory) |
| `metadata.version` as unquoted number | YAML treats `1.2` as float 1.2, not string | Quote it: `"1.2"` |
| Auxiliary `.md` files mistaken for skills | `references/notes.md` appears in `list` | Put non-skill `.md` files inside a skill directory (they become assets of the parent) |

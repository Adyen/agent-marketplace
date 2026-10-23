# agent-marketplace compared with APM and skills.sh

People who look at `agent-marketplace` often already know one of two other tools: Microsoft's [Agent Package Manager (APM)](https://microsoft.github.io/apm/) or Vercel's [`skills` CLI](https://github.com/vercel-labs/skills) and its directory at [skills.sh](https://skills.sh). All three read resources from Git and write them into the folders your coding tools read. They differ in what they treat as the unit you install, who decides what reaches a machine, and how much they control after the first install.

This page answers the questions people ask most often. The facts about APM and skills.sh come from their public documentation as of October 2026. Both projects move fast, so check their docs before you rely on a detail.

## The short answer

| | agent-marketplace | APM | `skills` CLI / skills.sh |
|---|---|---|---|
| What you install | Resources picked from a [registry](../glossary.md#registry) | Packages and their dependencies | Skills |
| Resource kinds | Skills, commands, subagents, hooks, MCP servers | Instructions, skills, prompts, agents, hooks, commands, plugins, MCP servers, LSP servers | Skills |
| Manifest | Up to four layered config files | One `apm.yml` per project, or a user manifest with `-g` | No dedicated manifest for basic `add`; `package.json` `skills` declarations for `experimental_sync` |
| Lockfile | Exact commit or Marketplace revision per source, plus a digest per resource | Exact ref and content hash per package | Project `skills-lock.json` with source, ref, and content hash per skill; `experimental_install` restores supported entries |
| Dependencies | Advisory [prerequisites](../reference/resource-prerequisites.md); nothing installs on its own | Resolves the full tree, including indirect dependencies | No APM-style resolution; `experimental_sync` syncs skills from installed npm dependencies and `package.json` `skills` declarations |
| Organization control | An organization config layer that adds or requires resources on every machine | A policy file that limits what may install | None |
| Hosted catalog | A Marketplace service you run, with ingestion policy and revocation | A curated index file in a Git repository | The public skills.sh directory and leaderboard |
| Coding tools | 8 (Claude Code, Codex, Cursor, Devin, Factory Droid, Gemini CLI, OpenCode, Windsurf) | More than ten, including GitHub Copilot, Kiro, and IntelliJ | 79, mostly by linking the same `SKILL.md` into each tool's skills folder |

## Is agent-marketplace a package manager like APM?

Not quite. APM borrows its model from npm: a package is the unit, `apm.yml` lists packages, and APM resolves each package's own dependencies into one tree. A package author decides what comes along with their package.

`agent-marketplace` treats a Git repository as a registry that holds many resources. You do not install the repository; you pick from it by name, by tag, with `all: true`, or through a [collection](../reference/collections.md). Nothing else installs unless you select it. A resource can name others it needs, but that only produces a warning with an install hint. What lands on a machine is always what someone selected, which makes it easy to see why a file is there.

APM handles large dependency graphs that `agent-marketplace` does not attempt. `agent-marketplace` makes it easy to take a subset of a big registry and to keep one person's or one team's choices on top of a shared default.

## How do organizations control what reaches a machine?

Both APM and `agent-marketplace` serve organizations, but they control different things.

APM's `apm-policy.yml` limits what may install. Rules pass down from enterprise to organization to repository, and each level can only tighten them. APM checks the policy at install time, including for MCP servers that arrive through other dependencies.

`agent-marketplace` delivers resources. Configuration comes in [four layers](./scopes-selection-and-precedence.md): organization (`/etc/agent-marketplace/`), user, repository, and personal-local. The organization layer can add resources to every machine, and `required: true` keeps a user or repository from dropping them. An organization lock decides when those machines move to new content; without one, every machine picks up new commits on its next `sync`. To limit what may install at all, an operator runs a hosted Marketplace and sets its ingestion policy.

The `skills` CLI has no organization layer. Each person or project runs `add` for what they want.

## What does the hosted Marketplace add?

All three tools have something called a marketplace or directory, and they mean different things.

- **skills.sh** is a public directory run by Vercel. It lists skills, ranks them with anonymous usage data that the `skills` CLI sends by default, and groups them into packs. `agent-marketplace` sends no telemetry unless you or your organization turn it on; see [telemetry and privacy](./telemetry-and-privacy.md).
- **An APM marketplace** is a curated list of packages that one Git repository publishes. `apm pack` builds it into a `marketplace.json` that Claude Code and other tools can also read.
- **An agent-marketplace Marketplace** is a service you run yourself (`agent-marketplace serve`) with a web UI and an HTTP API. It reads only the registries an operator approved, applies license, size, and hidden-content rules before anyone can install, gives every resource revision a fixed digest, and shows security findings that need consent before install. An operator can revoke a registry, a resource, or one revision, and a revoked pin then fails `sync`. See [Git registries and hosted Marketplaces](./git-registries-and-hosted-marketplaces.md) and the [Marketplace trust model](./marketplace-trust-model.md).

The cost of the last option is infrastructure: a database, a blob store, a worker, and a reverse proxy. A plain Git registry needs none of that, and `agent-marketplace` works with Git alone.

## How do updates and pinning compare?

All three write a lockfile, and all three can record a content hash. They differ in how far the lock goes.

APM pins every package, including indirect ones, to an exact ref and content hash, and `apm install --frozen` refuses to run when the lock and manifest disagree. The `skills` CLI records each skill's source, ref, and a content hash, in `skills-lock.json` for a project or `~/.agents/.skill-lock.json` for a user.

`agent-marketplace` pins each Git source to one exact commit and each Marketplace resource to one exact revision. A push to the registry changes nothing until someone moves the lock, and the moved lock goes through code review like any other diff. `update` refuses to move a lock past a selection that no longer exists upstream. [How upstream changes reach consumers](./how-upstream-changes-reach-consumers.md) covers each case, including the organization layer without a lock, which deliberately skips review.

## What happens to files the tool did not write?

This matters most for shared files such as `.claude/settings.json`, `.mcp.json`, or `.codex/config.toml`, which hold entries from several places.

`agent-marketplace` changes or removes only what it can prove it wrote: a symlink into its cache, a marked file, a copy listed in its manifest, or an entry recorded in its [ownership state](./shared-configuration-ownership.md). If you edit a managed copy, it keeps your bytes and warns on every sync. The cost is that edited copies and handwritten entries stay behind for you to clean up.

APM and the `skills` CLI also track what they installed, so they can update and remove it. This page does not compare how each handles a hand-edited file.

## Which coding tools does each support?

The `skills` CLI supports the most tools because a skill is a plain `SKILL.md` folder and most tools read the same layout. APM covers many tools and more resource kinds, including GitHub Copilot, Kiro, Antigravity, Grok, and IntelliJ.

`agent-marketplace` supports eight tools. For each one it converts every resource kind into that tool's own format: a Claude subagent becomes a Codex `.toml` file, and a hook becomes an entry in each tool's hook file. When a tool cannot express part of a resource, it reports a [diagnostic](../reference/diagnostics.md) instead of guessing. It supports Devin CLI and Factory Droid, which APM does not list. [Native harness materialization](./native-harness-materialization.md) explains the conversion.

## What does APM or skills.sh do that agent-marketplace does not?

- APM installs instructions (`AGENTS.md` and similar), prompts, and LSP servers, and compiles instructions for each tool.
- APM resolves indirect dependencies and checks a declarative policy file at install time.
- APM scans every install for hidden Unicode on the client. `agent-marketplace` runs hidden-content rules only when a hosted Marketplace reads a registry.
- APM and the `skills` CLI support more coding tools.
- The `skills` CLI can run a skill once without installing it (`skills use`), and can sync skills that ship inside npm packages.
- skills.sh gives a public place to find popular skills.

## Which one should I use?

- Use the **`skills` CLI** when you want to try public skills quickly on your own machine and do not need commands, hooks, or MCP servers.
- Use **APM** when your resources come as packages with their own dependencies, or when you need install-time policy across many repositories, or tools that only APM supports.
- Use **agent-marketplace** when a team or organization shares a few large registries, wants every machine and CI to get the same files from a reviewed lock, needs to push or require resources from the organization level, or wants a self-hosted catalog that can revoke a bad revision.

## Related documentation

- [How agent-marketplace works](./how-agent-marketplace-works.md)
- [Sources and resources](./sources-and-resources.md)
- [Scopes, selection, and precedence](./scopes-selection-and-precedence.md)
- [How upstream changes reach consumers](./how-upstream-changes-reach-consumers.md)
- [Git registries and hosted Marketplaces](./git-registries-and-hosted-marketplaces.md)
- [Harnesses and output paths reference](../reference/harnesses-and-output-paths.md)

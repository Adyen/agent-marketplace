# Documentation

`agent-marketplace` is a command-line tool that installs shared AI agent resources into your coding tools. A resource is a skill, command, subagent, hook, or MCP server. The tool downloads resources from Git registries or a hosted Marketplace and writes each one into the folders your coding tool reads. These docs call each coding tool a [harness](./glossary.md#harness). Seven are supported: Claude Code, Codex, Cursor, Devin, Factory Droid, Gemini CLI, and Windsurf.

## Start here

New to the tool? Follow these steps in order:

1. If you rarely use a terminal, read [Terminal basics](./terminal-basics.md) first.
2. [Install](./install.md) the binary.
3. Follow the [quick start](./quick-start.md). In about ten minutes you select a skill from a public registry, look at it in the terminal UI, and commit the configuration so teammates can reproduce the setup.

Look up unfamiliar words in the [glossary](./glossary.md). Then pick the section that matches your goal.

## I use agent resources in my projects

You want skills and other resources in your editor, and you want them to stay the same for everyone on the team.

The [user-journey map](./explanation/user-journeys.md) follows a resource from finding it to recovering it. It links to the guide for each stage.

| Goal | Read |
|---|---|
| Install one skill from Git, end to end, on Claude Code | [Tutorial: install your first skill](./tutorials/install-your-first-skill.md) |
| Find and install your first skill from an online Marketplace | [Tutorial: install your first Marketplace skill](./tutorials/install-your-first-marketplace-skill.md) |
| Read a resource's content and security findings, or inspect an older revision | [Browse and inspect Marketplace resources](./how-to/browse-marketplace-resources.md) |
| Add agent-marketplace to a repository the team shares | [Adopt in a repository](./how-to/adopt-agent-marketplace-in-a-repository.md) |
| Make CI prove every clone gets the same files | [Keep resources reproducible](./how-to/keep-resources-reproducible.md) |
| Figure out why `sync --check` is failing | [Troubleshoot sync drift](./how-to/troubleshoot-sync-drift.md) |
| Target more than one harness | [Configure multiple harnesses](./how-to/configure-multiple-harnesses.md) |
| Remove a resource, or keep one out of a team default | [Remove or override resources](./how-to/remove-or-override-resources.md) · [Personal repository overrides](./how-to/use-personal-repository-overrides.md) |
| Install from a curated Marketplace instead of a raw Git URL | [Install from a hosted Marketplace](./how-to/install-from-a-marketplace.md) |
| Understand what the tool does | [How agent-marketplace works](./explanation/how-agent-marketplace-works.md) |

Other guides for people who use resources: [install bundles](./how-to/install-resource-bundles.md), [regular files instead of symlinks](./how-to/write-regular-files-instead-of-symlinks.md), [local registries](./how-to/use-a-local-registry.md), [private Git registries](./how-to/configure-private-git-registries.md), [convert configuration before upgrading](./how-to/migrate-configuration.md), [convert a harness layout](./how-to/convert-a-harness-layout.md), [repair owned shared configuration](./how-to/repair-owned-shared-configuration.md), [back up machine resources](./how-to/back-up-and-restore-machine-resources.md), [clean the cache](./how-to/clean-the-cache-safely.md), [remove agent-marketplace](./how-to/remove-agent-marketplace-safely.md), [telemetry](./how-to/configure-telemetry.md).

## I publish resources for others

You maintain a Git repository of skills that other people install.

| Goal | Read |
|---|---|
| Create a registry from nothing and install from it | [Tutorial: create your first registry](./tutorials/create-your-first-registry.md) |
| Add a skill or command | [Add a resource to a registry](./how-to/author/add-a-resource-to-a-registry.md) |
| Make resources discoverable by tag and licensed | [Declare tags and a license](./how-to/author/declare-tags-and-licenses.md) |
| Declare what a resource needs to be installed | [Declare resource prerequisites](./how-to/author/declare-resource-prerequisites.md) |
| Ship a bundle consumers can install in one step | [Publish resource collections](./how-to/author/publish-resource-collections.md) |
| Catch broken resources before consumers do | [Validate a registry in CI](./how-to/author/validate-a-registry-in-ci.md) |
| Cut a release consumers can pin | [Version and release a registry](./how-to/author/version-and-release-a-registry.md) |
| Look up exactly which files are discovered | [Registry layout](./reference/registry-layout.md) · [Resource metadata](./reference/resource-metadata.md) · [Collections](./reference/collections.md) |

## I operate a Marketplace

You run the hosted service that indexes and curates registries for an organization.

| Goal | Read |
|---|---|
| See the whole thing working on your laptop | [Tutorial: run a local Marketplace](./tutorials/run-a-local-marketplace.md) |
| Add a registry from the admin panel for the first time | [Tutorial: add your first registry](./tutorials/add-your-first-marketplace-registry.md) |
| Deploy it for real | [Deploy a production Marketplace](./how-to/operator/deploy-a-production-marketplace.md) · [Test locally with Compose](./how-to/operator/test-locally-with-compose.md) · [Configure a reverse proxy](./how-to/operator/configure-a-reverse-proxy.md) |
| Review submissions and policy in a browser | [Enable and use the admin panel](./how-to/operator/enable-and-use-the-admin-panel.md) |
| Index a registry that needs credentials | [Connect a private registry](./how-to/operator/connect-a-private-registry.md) |
| Decide what is allowed in | [Manage ingestion policy](./how-to/operator/manage-ingestion-policy.md) |
| Pull something bad | [Revoke a resource revision](./how-to/operator/revoke-a-resource.md) |
| Keep it healthy and recoverable | [Reindex and monitor](./how-to/operator/reindex-and-monitor.md) · [Reset and reindex a registry](./how-to/operator/reset-and-reindex-a-registry.md) · [Back up and restore](./how-to/operator/back-up-and-restore.md) |
| Understand the moving parts and the trust boundary | [Marketplace architecture](./explanation/marketplace-architecture.md) · [Marketplace trust model](./explanation/marketplace-trust-model.md) |

## Reference

Exact descriptions of commands, files, and fields.

- CLI: [command line](./reference/cli.md) · [TUI](./reference/tui.md) · [environment variables](./reference/environment-variables.md) · [diagnostics](./reference/diagnostics.md) · [logging](./reference/logging.md) · [telemetry](./reference/telemetry.md)
- Consumer files: [repository configuration](./reference/configuration.md) · [configuration layers](./reference/configuration-layers.md) · [lockfiles](./reference/lockfiles.md) · [filesystem layout](./reference/filesystem-layout.md) · [harnesses and output paths](./reference/harnesses-and-output-paths.md)
- Registry files: [registry layout](./reference/registry-layout.md) · [resource metadata](./reference/resource-metadata.md) · [resource prerequisites](./reference/resource-prerequisites.md) · [collections](./reference/collections.md)
- Marketplace: [operator configuration](./reference/marketplace-operator-configuration.md) · [HTTP API](./reference/marketplace-http-api.md) · [health and metrics](./reference/marketplace-health-and-metrics.md) · [resource locators](./reference/marketplace-resource-locators.md)

## Explanation

Why it works this way.

- [How agent-marketplace works](./explanation/how-agent-marketplace-works.md)
- [Sources and resources](./explanation/sources-and-resources.md)
- [Scopes, selection, and precedence](./explanation/scopes-selection-and-precedence.md)
- [Native harness materialization](./explanation/native-harness-materialization.md)
- [Reproducibility and lockfiles](./explanation/reproducibility-and-lockfiles.md)
- [Shared configuration ownership](./explanation/shared-configuration-ownership.md)
- [Git registries and hosted Marketplaces](./explanation/git-registries-and-hosted-marketplaces.md)
- [Telemetry and privacy](./explanation/telemetry-and-privacy.md)
- [Marketplace architecture](./explanation/marketplace-architecture.md)
- [Marketplace trust model](./explanation/marketplace-trust-model.md)

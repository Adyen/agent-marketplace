# Documentation

`agent-marketplace` syncs shared AI agent resources from Git-based or local registries and materializes them into the native layout of each coding harness you use.

## Start here

- [Install](./install.md): get the binary onto your `PATH`.
- [Quick start](./quick-start.md): pick a harness, point a project at a registry, and toggle skills in the interactive UI.

## Tutorials

Step-by-step lessons that take you from nothing to a working result.

### Consumers

- [Install your first skill for Claude Code](./tutorials/install-your-first-skill.md)

### Registry authors

- [Create your first registry](./tutorials/create-your-first-registry.md)

### Marketplace operators

- [Run a local Marketplace](./tutorials/run-a-local-marketplace.md)

## How-to guides

Task-oriented instructions for a specific goal, assuming you already know the basics.

### Consumers

- [Adopt agent-marketplace in a repository](./how-to/adopt-agent-marketplace-in-a-repository.md)
- [Keep resources reproducible](./how-to/keep-resources-reproducible.md)
- [Troubleshoot sync drift](./how-to/troubleshoot-sync-drift.md)
- [Install resource bundles](./how-to/install-resource-bundles.md)
- [Remove or override resources](./how-to/remove-or-override-resources.md)
- [Use personal repository overrides](./how-to/use-personal-repository-overrides.md)
- [Configure multiple harnesses](./how-to/configure-multiple-harnesses.md)
- [Write regular files instead of symlinks](./how-to/write-regular-files-instead-of-symlinks.md)
- [Use a local registry](./how-to/use-a-local-registry.md)
- [Migrate configuration](./how-to/migrate-configuration.md)
- [Convert a harness layout](./how-to/convert-a-harness-layout.md)
- [Repair owned shared configuration](./how-to/repair-owned-shared-configuration.md)
- [Back up and restore machine resources](./how-to/back-up-and-restore-machine-resources.md)
- [Remove agent-marketplace safely](./how-to/remove-agent-marketplace-safely.md)
- [Clean the cache safely](./how-to/clean-the-cache-safely.md)
- [Configure private Git registries](./how-to/configure-private-git-registries.md)
- [Configure telemetry](./how-to/configure-telemetry.md)
- [Install a resource from a hosted Marketplace](./how-to/install-from-a-marketplace.md)

### Registry authors

- [Add a resource to a registry](./how-to/author/add-a-resource-to-a-registry.md)
- [Publish resource collections](./how-to/author/publish-resource-collections.md)
- [Declare tags and prerequisites](./how-to/author/declare-tags-and-prerequisites.md)
- [Validate a registry in CI](./how-to/author/validate-a-registry-in-ci.md)
- [Version and release a registry](./how-to/author/version-and-release-a-registry.md)

### Marketplace operators

- [Deploy a production Marketplace](./how-to/operator/deploy-a-production-marketplace.md)
- [Configure a reverse proxy](./how-to/operator/configure-a-reverse-proxy.md)
- [Connect a private registry](./how-to/operator/connect-a-private-registry.md)
- [Back up and restore a Marketplace](./how-to/operator/back-up-and-restore.md)
- [Manage ingestion policy](./how-to/operator/manage-ingestion-policy.md)
- [Revoke a resource](./how-to/operator/revoke-a-resource.md)
- [Reindex and monitor](./how-to/operator/reindex-and-monitor.md)

## Reference

Precise descriptions of commands, flags, files, and fields.

- [Command line](./reference/cli.md)
- [Repository configuration](./reference/configuration.md)
- [Configuration layers](./reference/configuration-layers.md)
- [Harnesses and output paths](./reference/harnesses-and-output-paths.md)
- [TUI](./reference/tui.md)
- [Lockfiles](./reference/lockfiles.md)
- [Filesystem layout](./reference/filesystem-layout.md)
- [Environment variables](./reference/environment-variables.md)
- [Diagnostics](./reference/diagnostics.md)
- [Logging](./reference/logging.md)
- [Telemetry](./reference/telemetry.md)
- [Marketplace operator configuration](./reference/marketplace-operator-configuration.md)
- [Marketplace HTTP API](./reference/marketplace-http-api.md)
- [Marketplace health and metrics](./reference/marketplace-health-and-metrics.md)
- [Marketplace resource locators](./reference/marketplace-resource-locators.md)
- [Registry layout](./reference/registry-layout.md)
- [Resource metadata](./reference/resource-metadata.md)
- [Collections](./reference/collections.md)

## Explanation

Discussions that build a mental model of how `agent-marketplace` works and why it is designed this way.

- [How agent-marketplace works](./explanation/how-agent-marketplace-works.md)
- [Scopes, selection, and precedence](./explanation/scopes-selection-and-precedence.md)
- [Native harness materialization](./explanation/native-harness-materialization.md)
- [Reproducibility and lockfiles](./explanation/reproducibility-and-lockfiles.md)
- [Shared configuration ownership](./explanation/shared-configuration-ownership.md)
- [Git registries and hosted Marketplaces](./explanation/git-registries-and-hosted-marketplaces.md)
- [Telemetry and privacy](./explanation/telemetry-and-privacy.md)
- [Marketplace architecture](./explanation/marketplace-architecture.md)
- [Marketplace trust model](./explanation/marketplace-trust-model.md)
- [Resource identity, revisions, and installed names](./explanation/resource-identity-revisions-and-installed-names.md)
- [Mirror and index-only acquisition](./explanation/mirror-and-index-only-acquisition.md)
- [Resource discovery and compilation](./explanation/resource-discovery-and-compilation.md)

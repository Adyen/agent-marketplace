import type { DefaultTheme } from 'vitepress'

// One entry per page, ordered by hand. Add a line here when adding a page
// under site/docs/. The same list drives llms.txt, so a page missing here
// fails the build.
export function docsSidebar(docsPath: string): DefaultTheme.SidebarItem[] {
  return [
    { text: 'Overview', link: docsPath },
    { text: 'Quick start', link: `${docsPath}quick-start` },
    { text: 'More install options', link: `${docsPath}install` },
    { text: 'Glossary', link: `${docsPath}glossary` },
    {
      text: 'Tutorials',
      items: [
        {
          text: 'Consumers',
          items: [
            { text: 'Install your first skill', link: `${docsPath}tutorials/install-your-first-skill` },
            { text: 'Install your first Marketplace skill', link: `${docsPath}tutorials/install-your-first-marketplace-skill` }
          ]
        },
        {
          text: 'Registry authors',
          items: [
            { text: 'Create your first registry', link: `${docsPath}tutorials/create-your-first-registry` }
          ]
        },
        {
          text: 'Marketplace operators',
          items: [
            { text: 'Run a local Marketplace', link: `${docsPath}tutorials/run-a-local-marketplace` },
            { text: 'Add your first registry', link: `${docsPath}tutorials/add-your-first-marketplace-registry` },
            { text: 'Run a Marketplace on Kubernetes', link: `${docsPath}tutorials/run-a-marketplace-on-kubernetes` }
          ]
        }
      ]
    },
    {
      text: 'How-to guides',
      items: [
        {
          text: 'Consumers',
          items: [
            {
              text: 'Find and install',
              collapsed: true,
              items: [
                { text: 'Browse and inspect Marketplace resources', link: `${docsPath}how-to/browse-marketplace-resources` },
                { text: 'Install from a hosted Marketplace', link: `${docsPath}how-to/install-from-a-marketplace` },
                { text: 'Install resource bundles', link: `${docsPath}how-to/install-resource-bundles` },
                { text: 'Use a local registry', link: `${docsPath}how-to/use-a-local-registry` },
                { text: 'Configure private Git registries', link: `${docsPath}how-to/configure-private-git-registries` }
              ]
            },
            {
              text: 'Set up repositories and harnesses',
              collapsed: true,
              items: [
                { text: 'Adopt in a repository', link: `${docsPath}how-to/adopt-agent-marketplace-in-a-repository` },
                { text: 'Keep resources reproducible', link: `${docsPath}how-to/keep-resources-reproducible` },
                { text: 'Use personal repository overrides', link: `${docsPath}how-to/use-personal-repository-overrides` },
                { text: 'Keep a local change to a synced resource', link: `${docsPath}how-to/keep-local-changes-to-synced-resources` },
                { text: 'Remove or override resources', link: `${docsPath}how-to/remove-or-override-resources` },
                { text: 'Configure multiple harnesses', link: `${docsPath}how-to/configure-multiple-harnesses` },
                { text: 'Write regular files instead of symlinks', link: `${docsPath}how-to/write-regular-files-instead-of-symlinks` }
              ]
            },
            {
              text: 'Troubleshoot and repair',
              collapsed: true,
              items: [
                { text: 'Troubleshoot sync drift', link: `${docsPath}how-to/troubleshoot-sync-drift` },
                { text: 'Handle a renamed or removed resource', link: `${docsPath}how-to/handle-renamed-or-removed-resources` },
                { text: 'Respond to a revoked resource', link: `${docsPath}how-to/respond-to-a-revoked-resource` },
                { text: 'Repair owned shared configuration', link: `${docsPath}how-to/repair-owned-shared-configuration` },
                { text: 'Clean the cache safely', link: `${docsPath}how-to/clean-the-cache-safely` }
              ]
            },
            {
              text: 'Maintain and uninstall',
              collapsed: true,
              items: [
                { text: 'Back up and restore machine resources', link: `${docsPath}how-to/back-up-and-restore-machine-resources` },
                { text: 'Review and roll back an update', link: `${docsPath}how-to/review-and-roll-back-updates` },
                { text: 'Convert configuration before upgrading', link: `${docsPath}how-to/migrate-configuration` },
                { text: 'Configure telemetry', link: `${docsPath}how-to/configure-telemetry` },
                { text: 'Remove agent-marketplace safely', link: `${docsPath}how-to/remove-agent-marketplace-safely` }
              ]
            }
          ]
        },
        {
          text: 'Registry authors',
          items: [
            { text: 'Add a resource to a registry', link: `${docsPath}how-to/author/add-a-resource-to-a-registry` },
            { text: 'Publish a hook with support files', link: `${docsPath}how-to/author/publish-a-hook-with-support-files` },
            { text: 'Publish resource collections', link: `${docsPath}how-to/author/publish-resource-collections` },
            { text: 'Declare tags and a license', link: `${docsPath}how-to/author/declare-tags-and-licenses` },
            { text: 'Declare resource prerequisites', link: `${docsPath}how-to/author/declare-resource-prerequisites` },
            { text: 'Validate a registry in CI', link: `${docsPath}how-to/author/validate-a-registry-in-ci` },
            { text: 'Version and release a registry', link: `${docsPath}how-to/author/version-and-release-a-registry` },
            { text: 'Rename or retire a resource', link: `${docsPath}how-to/author/rename-or-retire-a-resource` }
          ]
        },
        {
          text: 'Marketplace operators',
          items: [
            {
              text: 'Deploy',
              collapsed: true,
              items: [
                { text: 'Deploy a production Marketplace', link: `${docsPath}how-to/operator/deploy-a-production-marketplace` },
                { text: 'Test locally with Compose', link: `${docsPath}how-to/operator/test-locally-with-compose` },
                { text: 'Configure a reverse proxy', link: `${docsPath}how-to/operator/configure-a-reverse-proxy` },
                { text: 'Enable and use the admin panel', link: `${docsPath}how-to/operator/enable-and-use-the-admin-panel` },
                { text: 'Roll out organization configuration', link: `${docsPath}how-to/operator/roll-out-organization-configuration` }
              ]
            },
            {
              text: 'Manage content',
              collapsed: true,
              items: [
                { text: 'Connect a private registry', link: `${docsPath}how-to/operator/connect-a-private-registry` },
                { text: 'Manage ingestion policy', link: `${docsPath}how-to/operator/manage-ingestion-policy` },
                { text: 'Revoke a resource', link: `${docsPath}how-to/operator/revoke-a-resource` }
              ]
            },
            {
              text: 'Run and recover',
              collapsed: true,
              items: [
                { text: 'Reindex and monitor', link: `${docsPath}how-to/operator/reindex-and-monitor` },
                { text: 'Reset and reindex a registry', link: `${docsPath}how-to/operator/reset-and-reindex-a-registry` },
                { text: 'Back up and restore a Marketplace', link: `${docsPath}how-to/operator/back-up-and-restore` }
              ]
            }
          ]
        }
      ]
    },
    {
      text: 'Reference',
      items: [
        {
          text: 'CLI',
          collapsed: true,
          items: [
            { text: 'Command line', link: `${docsPath}reference/cli` },
            { text: 'TUI', link: `${docsPath}reference/tui` },
            { text: 'Environment variables', link: `${docsPath}reference/environment-variables` },
            { text: 'Diagnostics', link: `${docsPath}reference/diagnostics` },
            { text: 'Logging', link: `${docsPath}reference/logging` },
            { text: 'Telemetry', link: `${docsPath}reference/telemetry` }
          ]
        },
        {
          text: 'Consumer files',
          collapsed: true,
          items: [
            { text: 'Repository configuration', link: `${docsPath}reference/configuration` },
            { text: 'Configuration layers', link: `${docsPath}reference/configuration-layers` },
            { text: 'Lockfiles', link: `${docsPath}reference/lockfiles` },
            { text: 'Filesystem layout', link: `${docsPath}reference/filesystem-layout` },
            { text: 'Harnesses and output paths', link: `${docsPath}reference/harnesses-and-output-paths` }
          ]
        },
        {
          text: 'Registry files',
          collapsed: true,
          items: [
            { text: 'Registry layout', link: `${docsPath}reference/registry-layout` },
            { text: 'Resource metadata', link: `${docsPath}reference/resource-metadata` },
            { text: 'Resource prerequisites', link: `${docsPath}reference/resource-prerequisites` },
            { text: 'Collections', link: `${docsPath}reference/collections` },
            { text: 'Hook and tool names', link: `${docsPath}reference/hook-and-tool-names` }
          ]
        },
        {
          text: 'Marketplace',
          collapsed: true,
          items: [
            { text: 'Marketplace operator configuration', link: `${docsPath}reference/marketplace-operator-configuration` },
            { text: 'Marketplace HTTP API', link: `${docsPath}reference/marketplace-http-api` },
            { text: 'Marketplace health and metrics', link: `${docsPath}reference/marketplace-health-and-metrics` },
            { text: 'Marketplace resource locators', link: `${docsPath}reference/marketplace-resource-locators` }
          ]
        }
      ]
    },
    {
      text: 'Explanation',
      items: [
        {
          text: 'Start here',
          collapsed: true,
          items: [
            { text: 'User journeys', link: `${docsPath}explanation/user-journeys` },
            { text: 'How agent-marketplace works', link: `${docsPath}explanation/how-agent-marketplace-works` },
            { text: 'Sources and resources', link: `${docsPath}explanation/sources-and-resources` },
            { text: 'Compared with APM and skills.sh', link: `${docsPath}explanation/compared-with-other-tools` }
          ]
        },
        {
          text: 'Core model',
          collapsed: true,
          items: [
            { text: 'Scopes, selection, and precedence', link: `${docsPath}explanation/scopes-selection-and-precedence` },
            { text: 'Native harness materialization', link: `${docsPath}explanation/native-harness-materialization` },
            { text: 'Reproducibility and lockfiles', link: `${docsPath}explanation/reproducibility-and-lockfiles` },
            { text: 'How upstream changes reach consumers', link: `${docsPath}explanation/how-upstream-changes-reach-consumers` },
            { text: 'Shared configuration ownership', link: `${docsPath}explanation/shared-configuration-ownership` }
          ]
        },
        {
          text: 'Marketplace and privacy',
          collapsed: true,
          items: [
            { text: 'Git registries and hosted Marketplaces', link: `${docsPath}explanation/git-registries-and-hosted-marketplaces` },
            { text: 'Marketplace architecture', link: `${docsPath}explanation/marketplace-architecture` },
            { text: 'Marketplace trust model', link: `${docsPath}explanation/marketplace-trust-model` },
            { text: 'Telemetry and privacy', link: `${docsPath}explanation/telemetry-and-privacy` }
          ]
        }
      ]
    }
  ]
}

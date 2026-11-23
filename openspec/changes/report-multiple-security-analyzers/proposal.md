# Proposal

## Why

The current Security tab makes broad warnings and substantive security findings look alike. Showing independent reports lets readers see which tool found what, and distinguish findings from limits in what a tool could inspect.

## What Changes

- Keep the built-in Marketplace assessment as the authority on policy and install availability.
- Run the built-in analyzer and an isolated, offline SkillSpector scanner for Marketplace skills by default in the supplied Compose and Helm deployments. Keep the external scanner out of the consumer CLI.
- Show a separate report for each tool on the resource detail page, with version, coverage status and findings where available. Distinguish an incomplete or unrun scan from a scan without findings.
- Preserve existing findings, resource identity, installation and revocation behavior. Do not expose SkillSpector's aggregate risk score as a Marketplace verdict.

## Capabilities

### New Capabilities

### Modified Capabilities

- `marketplace-analysis`: Multiple analyzer reports for an exact skill revision with independent status and unchanged admission policy.
- `marketplace-api`: Additive advisory report data in resource assessments with backward-compatible identity and reads.
- `marketplace-web-ui`: Resource detail groups security reports by tool and presents Marketplace policy separately.
- `marketplace-delivery-security`: Supplied deployments enable SkillSpector with isolated execution and no outbound network access.

## Impact

Marketplace ingestion and assessment storage, public API and generated web client types, resource detail rendering, release images, and Compose and Helm deployments. Standalone Marketplace servers without the runner and existing assessments remain usable.

## Release note

The Marketplace resource detail page shows security reports by tool and makes clear when a scan was incomplete or did not run. Supplied Marketplace deployments run built-in analysis and SkillSpector for skills by default; SkillSpector does not change install availability.

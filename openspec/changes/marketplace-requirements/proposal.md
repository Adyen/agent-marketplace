## Why

Marketplace resources can declare `metadata.requires`, but the Marketplace neither records nor exposes those prerequisites, and clients look up a Marketplace resource's prerequisites as if it came from a Git source. Release N also has to read the acquisition-lock requires edges that release N+1 will write, so mixed-version teams keep working.

## What Changes

- Index each published resource revision's prerequisites, and record revisions published before indexing as unindexed rather than as having none.
- Add `requires` and `required_by` to the resource detail response, limited to the same Marketplace, with explicit visibility, listing, redaction, and source-root path-hint rules. Search and revision responses are unchanged.
- Look up a Marketplace resource's unmet prerequisite only in the requiring resource's configured Marketplace entry: by default its own registry, or a registry bound in that entry when a registry hint names one. Use the requiring pin's exact registry commit when it has one, and the current revision otherwise.
- Read acquisition-lock requires edges, `selected_by` entries carrying a non-collection `kind`, and treat resources they hold as implied or previewed, never as user selections.
- Release N records no lookup result. It writes no requires edge and creates no prerequisite pin. A found candidate is previewed, and when expansion is enabled it is reported as unmet with the new reason `unrecorded`. A lock rewrite drops requires edges whose parent changed and prunes pins only those edges held. Refreshing implied Marketplace pins on update, and selecting new Marketplace candidates, wait for the release that writes requires edges.
- Removing a Marketplace prerequisite that a selected resource still requires succeeds: it removes the lock record that requires edges hold, and when expansion is enabled it writes the exact Marketplace exclusion.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `marketplace-publication`: Index prerequisites for each resource revision, keep the indexed and unindexed states distinct, and redact stored hints.
- `marketplace-api`: Add resource detail `requires` and `required_by` with resolution, visibility, completeness, and redaction rules.
- `acquisition-lock-codec`: Accept and canonicalize requires edges in Marketplace `selected_by`, and reject malformed ones.
- `resource-prerequisites`: Add Marketplace prerequisite lookup, requires-edge provenance, the `unrecorded` reason, and the release-N no-recording boundary.
- `resource-removal`: Cover still-required Marketplace prerequisites held by requires edges.

## Impact

- Marketplace ingest and the SQLite and Postgres metadata stores (one new nullable column).
- The public resource detail endpoint, `api/openapi.yaml`, and the generated web API types.
- The Marketplace client, the acquisition-lock codec, the lock-to-config admission and lock merge paths, the resolver, and removal.
- Site reference documentation.

Release N clients stay compatible with the latest release because they write no lock format change. Locks that a later release writes with requires edges cannot be read by releases older than N.

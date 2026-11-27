# Proposal

## Why

Hosted Place package revisions can have indexed prerequisites in Marketplace storage while the exact-revision response omits them. Online and cache-only clients then cannot construct the declared dependency graph, so they must not treat unknown metadata as an empty graph.

## What Changes

- Expose the exact stored prerequisite declarations on opted-in, available exact Place package revision responses only.
- Refuse Place package archive acquisition when prerequisite indexing is unknown; keep explicit empty arrays distinct from missing or `null`.
- Preserve existing latest-detail resolution, ordinary resource responses, storage, lock identity, and graph planning.

## Capabilities

### New Capabilities

### Modified Capabilities

- `marketplace-api`: define the exact Place revision prerequisite response and its eligibility.
- `target-profile-place`: require known prerequisite evidence before online or cache-only package acquisition.

## Impact

Changes are limited to the Marketplace API DTO/handler, Place client/cache acquisition, OpenAPI documentation, and focused tests. No storage schema, lock format, or dependency graph identity changes.

## Release note

Exact hosted Place package revisions now include their published prerequisite declarations. Place acquisition refuses when that metadata is unknown and directs users to refresh the Marketplace revision.

---
status: accepted
---

# Separate Marketplace identity from installed names and source locations

A resource is identified in a Marketplace by an immutable Source ID plus kind and source-local name, while users normally materialize it under its short source-local name. Repository moves preserve Source IDs, canonical source slugs may change, and old slugs remain permanent aliases. Installed-name collisions stop installation and require an explicit `--as` alias instead of leaking full Marketplace coordinates into every harness or silently renaming resources.

## Consequences

Consumer configuration stores the Marketplace instance ID, canonical endpoint, Source ID, exact resource revision, digest, and optional installed name. Human-facing commands may use a local Marketplace alias and source slug, but those conveniences are resolved before portable configuration is written.

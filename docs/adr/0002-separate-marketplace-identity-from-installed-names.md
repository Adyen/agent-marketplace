---
status: accepted
---

# Separate Marketplace identity from installed names and registry locations

A resource is identified in a Marketplace by an immutable Registry ID plus kind and registry-local name, while users normally materialize it under its short registry-local name. Repository moves preserve Registry IDs, canonical registry slugs may change, and old slugs remain permanent aliases. Installed-name collisions stop installation and require an explicit `--as` alias instead of leaking full Marketplace coordinates into every harness or silently renaming resources.

## Consequences

Consumer configuration stores the Marketplace instance ID, canonical endpoint, Registry ID, exact resource revision, digest, and optional installed name. Human-facing commands may use a local Marketplace alias and registry slug, but those conveniences are resolved before portable configuration is written.

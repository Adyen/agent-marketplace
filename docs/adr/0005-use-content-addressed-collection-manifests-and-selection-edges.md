---
status: accepted
---

# Use content-addressed collection manifests and selection edges

A collection revision is a content-addressed, unordered canonical manifest that preserves exact direct includes and a deduplicated expansion of exact non-collection leaves. Installation persists both the collection pin and its leaf pins; each leaf records whether it is selected directly or by one or more exact collection revisions.

## Consequences

Nested collections are expanded by the Marketplace, cycles and conflicting revisions of one coordinate are rejected, and exact duplicates are deduplicated while retaining their inclusion paths. Mirror and index-only leaves may coexist. Install and update acquire and verify the complete new leaf set before atomically changing configuration, and removing one selector cannot remove a pin still selected elsewhere.

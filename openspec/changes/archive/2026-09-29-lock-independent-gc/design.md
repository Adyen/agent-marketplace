## Context

GC needs local reachability and must not acquire unrelated Marketplace resources. See proposal.md and specs/cache-garbage-collection/spec.md.

## Goals / Non-Goals

**Goals:** Compute retention from authored roots and verified deployed evidence; block deletion when relevant evidence is invalid.

**Non-Goals:** Treat absent organization acquisition locks as errors or fetch remote data to repair them.

## Decisions

- Build a local retention union before deleting any cache object. Streaming deletions before complete validation would risk removing a still-reachable object.
- Keep collection of Git and Marketplace caches in one preflight so malformed relevant evidence blocks both deletions.
- Do not materialize virtual collections as independent resource artifacts.

## Risks / Trade-offs

- Conservative retention may leave extra cache entries when evidence is incomplete; this is safer than deleting a referenced exact resource.

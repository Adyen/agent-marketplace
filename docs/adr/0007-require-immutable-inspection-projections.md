---
status: accepted
---

# Require immutable inspection projections

Every published resource revision, including index-only revisions, has a bounded immutable inspection projection so users can inspect it before installation without downloading or decoding an artifact in the browser. The Marketplace serves canonical text and contained assets; Nuxt sanitizes rendered Markdown and provides escaped raw views.

## Consequences

An exact-revision inspection manifest lists the primary path and every file's canonical path, digest, media type, size, inspection class, and findings; textual files and permitted raster images are fetched separately by digest with fixed allowlisted media types and `nosniff`. Binary files expose identity, size, type, digest, and findings without a body URL when rendering is prohibited; SVG is not rendered inline and remote images never load. Findings address canonical paths and half-open UTF-8 byte ranges. Existing source-policy file-count, per-file, and artifact-size limits bound the complete projection, and publication fails rather than truncating it. Projection bytes are immutable-cacheable, while effective revocation is fetched and evaluated separately.

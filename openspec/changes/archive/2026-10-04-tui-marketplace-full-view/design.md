## Context

Installed mirror skills with supporting files resolve to a staging directory containing those files but no primary Markdown definition. The canonical skill IR is already attached to the resolved row. Archive-backed skills already synthesize `SKILL.md`. Discovery rows have no local source path; the Marketplace client already verifies inspection manifests, revision identity, file media types and content digests. The detail request generation guards against superseded responses.

Full view composes a fixed-width tree and preview. Baseline measurement shows Lip Gloss already fits the rendered panes to their allocated columns, so long bundle labels do not displace the preview or its mouse-selection coordinates. The long-label regressions are retained as witnesses of that existing behavior rather than evidence of a new render correction.

## Goals / Non-Goals

Restore complete installed skill browsing, enable read-only inspection before installation, and align displayed preview text with mouse selection. Preserve existing scroll routing and clipboard boundaries.

No tree/list text selection, new gestures, installation/materialization behavior changes, server API changes, or terminal configuration changes.

## Decisions

### Installed primary content is a virtual bundle member

Keep filesystem-backed supporting files under the existing safe bundle root. For an installed Marketplace skill whose staging tree lacks `SKILL.md`, compile its attached canonical IR into the existing Factory Markdown form and insert one virtual primary member. Reuse bundle rendering with a virtual-member override and safe filesystem fallback only for trees with a real root. Missing/invalid roots remain errors. Do not write into the staging directory or change the resolver/materializer.

### Discovery uses verified inspection, not acquisition

On Enter for discovery-only skills, issue an asynchronous exact-revision inspection command using the existing Marketplace client and `requestDetail` generation. Verify well-known instance identity against the discovery endpoint before content access. Reject incomplete exact coordinates. Load the primary content and manifest, enforce discovery revision/content-digest identity, and build the tree from the verified manifest. Use a separate result message carrying generation, row key, tree, rendered primary content, width and error. The model accepts it only for the active detail identity and generation.

Fetch supporting text files lazily on selection through the existing selected-file command path, with bounded context timeout and file digest verification. Binary/metadata-only files show identity or unsupported-preview information rather than bytes. Keep the manifest/client data immutable in commands; no asynchronous mutation of model trees or maps. Retain the frozen manifest identity when validating a file response. Do not call install, prepare-install, sync, config or acquisition lock writers, and do not read registry-relative source paths. Existing non-skill metadata details remain unchanged.

On resize, reflow accepted primary/file content at the current preview width; reject old-width presentation without dropping the accepted verified tree. Exit, resource replacement and newer detail generations supersede outstanding responses. Same-resource snapshot refresh must not discard an accepted inspection tree merely because its row still lacks a local path.

### Rendering continues to honor the allocated tree width

Exercise the long-root case through the real full-view composition and frame-paced mouse path. Baseline verification showed the existing layout already constrains the tree and preview to their allocated dimensions, so no render correction is introduced; the tests remain as regression witnesses. Do not change selection coordinates or expand selectable regions. Keep selection based on the last displayed frame.

## Risks / Trade-offs

Inspection introduces read-only network work on Enter for skills. Bound requests and preserve a responsive loading/error state. Offline or older services without inspection support must fail visibly; do not fabricate source content or fall back to installation.

Do not eagerly fetch every supporting file. Lazy reads avoid unnecessary network work and aggregate memory growth. No persistent inspection cache is added.

## Verification

Start with regressions for staged-directory `SKILL.md`, long root labels at50/80columns with real frame-paced mouse messages, and exact discovery inspection via local HTTP fixtures. Assert primary and support content, stale-response isolation, invalid identity/digest/error cases, and byte-identical config/lock fixtures with no install/sync callbacks. Exercise wheel and drag selection together in a direct real-terminal journey. Retain before/after captured output for the same long-label marketplace scenario. Measure touched render/input benchmarks before and after; retain a benchmark for tree construction/render if none covers the changed path.

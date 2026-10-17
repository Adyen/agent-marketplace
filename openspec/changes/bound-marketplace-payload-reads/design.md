# Design

## Existing Ceilings, Earlier Enforcement

Inspection and content readers use one internal bounded-read boundary. With a positive, representable ceiling, built-in stores use their existing bounded-read capability and consume at most the ceiling plus one probe byte. Inspection manifests retain their fixed serialized ceiling; content retains the configured file ceiling; registry and collection manifests retain their existing ceiling derived from the artifact limit. Archive delivery and decompression remain separate.

Nonpositive content ceilings keep their existing unlimited meaning. A maximum-int64 ceiling cannot admit a representable Go byte slice larger than itself and retains the legacy read path rather than overflowing a probe-byte calculation.

The existing blob interface remains unchanged. Custom stores without the bounded capability keep the legacy read path and post-read size check; the server cannot impose streaming confinement on an interface returning an already-materialized byte slice. This compatibility path is not evidence that arbitrary custom stores perform bounded reads.

## Compatibility

Size refusals keep each route's existing status and message. Not-found errors, availability decisions, manifest/content verification, final-versus-placeholder archive identity, and immutable success responses remain unchanged. In particular, an oversized registry manifest retains its existing generic server-error response rather than introducing a new protocol status.

## Verification

Inert HTTP fixtures count the selected backend method and bytes consumed. Oversized GET/HEAD requests must use bounded reads, stop after one probe byte and avoid immutable success caching. Healthy content and the existing inspection/archive suites retain response and identity controls. Before/after healthy-read benchmarks measure the cost of moving size enforcement into the backend read.

## Why

Ordered dense text findings repeatedly convert offsets by rescanning the entire preceding content. Bounded inert comment fixtures show roughly quadratic scaling in this conversion even though result ordering and positions are deterministic.

## What Changes

- Reuse scan-local rune/line state in ordered HTML-comment, invisible-character, bidi and terminal-control finding loops.
- Preserve source file, finding identity/order/count and one-based rune locations, including Unicode and malformed-byte handling.
- Keep other scanner and ingest offset paths unchanged; introduce no global cache or processing threshold.

## Capabilities

### Modified Capabilities

- `marketplace-analysis`: dense ordered finding source-location fidelity without redundant prefix work.

## Impact

Confirmed repeated finding-offset conversion work under #215/#434. No new finding/input budget, graph policy, source schema, scanner detection rule or service execution.

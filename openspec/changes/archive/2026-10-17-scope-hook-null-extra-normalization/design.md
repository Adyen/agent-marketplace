## Decision

Consume the serializer's null `Extra` field only when decoding `ir.Hook`.
Other kinds retain the historical unknown-field mapping used by native emitters.
Do not change TUI expectations or historical skill output to accommodate an
over-broad hook fix.

## Verification

Check hook omission and non-hook preservation separately, rerun affected package
validation, the Marketplace public gate, and every real-terminal TUI journey.

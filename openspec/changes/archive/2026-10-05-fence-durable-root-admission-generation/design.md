## Context

Canonical pathname containment does not distinguish two ordinary directory generations at the same path. The existing native Gemini race witness replaces the root with a symbolic alias, but does not cover an ordinary replacement containing identical settings.

## Decisions

- Capture an existing admitted canonical root using strict descriptor-relative traversal and retain its directory object identity.
- Close admission descriptors after capture. Store has no lifecycle Close API, so retaining descriptors for every preview or status Store would introduce resource leaks.
- Compare device/inode object identity, not directory mode, size, or modification time. Legitimate aggregate installation changes directory metadata.
- At ReadAggregate capture, reject a different existing root or a disappeared previously admitted root with the existing generation-conflict identity before reading payloads.
- A root genuinely absent at admission remains compatible with later authorized directory creation. Do not create roots or state from Open, and do not add persisted inode ownership.
- Apply the check in the shared Store reader, not a Gemini-only workaround. Preserve canonical resolution and strict no-follow traversal on reads.

## Risks and Limits

This closes the observed admission-to-read ordinary replacement gap. It does not exclude an uncooperative writer after validation, make all output transactional, or persist root generation across invocations. Capture and validation failures remain fail-closed. Missing-root creation remains explicitly supported.

## Validation

- Add a native Gemini public Run regression that replaces the root with an ordinary copied directory after actual lock acquisition and preserves foreign and retained payload bytes and metadata.
- Prove restored-original-root sync and fresh missing-root installation work.
- Add Store reader controls for ordinary replacement, optional absence, later creation, and legitimate same-directory writes.
- Measure affected Store admission/scoped read benchmarks before and after, with disk work and timing scope stated.

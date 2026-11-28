## Context

The Claude Plugins API exposes separate latest and served pointers and an explicit pin flag. A stored version is not necessarily served. File-mode Marketplace registries use a fenced registry head instead of a publication ID; both backends already restrict discovery to that head.

## Decisions

- Persist the reported pin flag with the version IDs. Read the uploaded version directly when the served pointer differs. Prefer the scan message while the uploaded version is processing.
- Recover an ambiguous registration resend without another write only when the served version verifies against the pending archive. For a different version ID require the latest pointer and the configured API-key actor. Do not carry resend permission into another pass.
- Leave the transport client's read retry in place. Return a write throttle to the leased worker without an inline retry so each outbound write goes through pacing.
- Treat `pause` as acquisition control, not withdrawal. Match eligibility to whether the managed registry remains published.
- Bound validation work from ZIP headers, preserve stable path diagnostics, and reject inline credential payloads before interpreting their values.

## Risks

- The pin flag uses an additive metadata migration. Existing rows default to false until the next successful refresh.
- A follow-up read failure can leave a registration pending until a later read verifies the served version. Resending across passes is out of scope because the vendor does not specify how long a retry hint remains valid.

## Why

Issue #770 demonstrates that one invalid UTF-8 byte disables all hidden-content
checks for a support file and makes its inspection body unavailable. An
extensionless script can therefore receive no finding despite concealing
terminal escapes, bidi controls, or encoded instructions.

## What Changes

- Emit `marketplace.find.invalid-utf8` for non-UTF-8 support files unless their
  bytes match an explicitly recognized binary signature.
- Reject that finding under the default public policy.
- Retain binary text-scan exemptions, independent asset checks, original
  artifact bytes, and existing inspection restrictions.
- Increment the analyzer version so reassessments identify the changed rules.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `marketplace-analysis`: Non-UTF-8 support files cannot silently bypass text
  analysis without a recognized binary signature.

## Impact

The shared analyzer affects every resource kind and both mirror and index-only
ingestion. No consumer configuration, acquisition-lock, artifact, inspection,
or HTTP API schema changes.

## Release note

Marketplace analysis now rejects non-UTF-8 support files unless their bytes
match a recognized binary signature. Unknown non-UTF-8 content receives an
explicit finding instead of silently bypassing hidden-content analysis.

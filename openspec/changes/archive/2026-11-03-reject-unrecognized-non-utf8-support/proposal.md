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

## Release Planning

This intentionally breaks the default Marketplace acceptance policy for resources
whose unrecognized non-UTF-8 support files were previously installable. Schedule
the change for a maintainer-selected major release, not a patch or minor release;
the release owner chooses its version and timing. Analyzer version `1.2.1`
identifies the detection rules, not the CLI or Marketplace product release.
This PR does not change the product version or create a release tag.

## Migration And Rollout

1. Publishers should review resources containing extensionless support scripts,
   legacy-encoded reference text, or other non-UTF-8 support files without a
   recognized binary signature. Re-encode intended text as UTF-8 using its
   original encoding, review the result, and fix remaining hidden-content
   findings. Replace unsupported binary assets with an appropriate recognized
   format or remove them and update the resource's references. Renaming a file
   or adding a signature is not remediation.
2. Commit corrected resources to the configured registry ref before the operator
   deploys the major release. Valid UTF-8 content and recognized binary
   exemptions retain their existing analysis and independent asset checks.
3. After deploying the new analyzer, operators should run
   `agent-marketplace serve reindex <registry-slug-or-id> --config <path>` using
   the worker's operator configuration, credentials, network access, and Git.
   This reassesses resources at the configured source ref; historical revisions
   absent from that source are not automatically rebuilt. A destructive registry
   reset is not required.
4. Confirm that corrected resources have non-rejecting assessments before asking
   consumers to update their pinned revisions. Uncorrected files receive
   `marketplace.find.invalid-utf8` with a reject outcome. Reindexing does not
   rewrite existing installations or reanalyze verified offline caches.

## Release note

Breaking: Marketplace resources with non-UTF-8 support files lacking a recognized
binary signature, including previously installable extensionless scripts and
legacy-encoded text, are now rejected under the default policy when analyzed or
reassessed. Before upgrading, publishers must review and re-encode intended text
as UTF-8, fix remaining hidden-content findings, and replace or remove unsupported
binary assets. After deploying the maintainer-selected major release, operators
must run `agent-marketplace serve reindex <registry-slug-or-id> --config <path>`
for affected registries to reassess their configured source revisions; existing
installations are not rewritten automatically.

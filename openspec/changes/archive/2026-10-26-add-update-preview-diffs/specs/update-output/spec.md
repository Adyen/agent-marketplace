## MODIFIED Requirements

### Requirement: A missing Marketplace resource does not block other pin updates

When the Marketplace answers the latest-revision lookup for a user-managed
Marketplace pin with HTTP 404, an applying `update` invocation (one without
`--dry-run`) SHALL treat that resource as missing rather than aborting. It SHALL
keep the missing pin at its current pinned revision with its lock entry
unchanged, plan, update, lock, and sync every other pin as it would otherwise,
and write its normal update report. It SHALL then exit with status 1 and an
error that names every missing resource by Marketplace URL, registry, kind, and
name, and that tells the user to remove the old pin with
`agent-marketplace remove <installed name>` and install the resource under its
new name. This applies to resource pins and collection pins alike.

A preview SHALL instead keep all persistent state unchanged, report the missing
pins and the other proposed updates, and exit with status 1 using the same
resource identities and remediation. A missing pin whose current revision is
retained SHALL have no proposed content change; its absent latest candidate
SHALL NOT be treated as a diff-generation failure for other eligible updates.

#### Scenario: Missing resource is kept while other pins update

- **WHEN** `update` runs with one Marketplace pin whose latest-revision lookup returns 404 and another pin that has a newer revision
- **THEN** it SHALL advance the other pin in the lock and harness outputs and write the update report
- **AND** it SHALL leave the missing pin's lock entry byte-identical
- **AND** it SHALL exit with status 1 and an error naming the missing resource's Marketplace URL, registry, kind, and name with the remove-and-install hint

#### Scenario: Lock and outputs agree after a partial update

- **WHEN** `update` has kept a missing Marketplace resource and updated other pins
- **THEN** a following `sync --check` SHALL report no drift

#### Scenario: Only missing resources change nothing

- **WHEN** every Marketplace pin that `update` checks returns 404 for its latest-revision lookup and no Git registry is configured
- **THEN** `update` SHALL not stage or sync anything and SHALL leave configuration, lock, and harness outputs byte-identical
- **AND** it SHALL name each missing resource and exit with status 1

#### Scenario: Every missing resource is named

- **WHEN** more than one Marketplace pin returns 404 for its latest-revision lookup during one `update`
- **THEN** the error SHALL name every missing resource, not only the first one checked

#### Scenario: Missing collection is kept

- **WHEN** a Marketplace collection pin's latest-revision lookup returns 404 during `update`
- **THEN** `update` SHALL keep that collection pin and its members unchanged, continue with the other pins, name the collection in the error, and exit with status 1

#### Scenario: Missing resource preview retains state and reports other candidates

- **GIVEN** one Marketplace pin whose latest-revision lookup returns 404 and another with a newer revision
- **WHEN** `update --dry-run --include-diff` runs
- **THEN** it SHALL keep both pins and installed content unchanged, name the missing resource with the existing remediation, show only the other pin's proposed diff, and exit 1

#### Scenario: Applied diffs retain partial update behavior

- **GIVEN** one missing Marketplace resource and another eligible resource with changed content
- **WHEN** `update --include-diff` runs
- **THEN** it SHALL update the eligible resource and report its applied diff, identify the retained missing resource without inventing a diff for it, and exit 1

## ADDED Requirements

### Requirement: Content diffs are explicitly requested

`update --include-diff` SHALL apply its prepared updates without an approval
prompt and include the corresponding resource diffs in its result report.
`update --dry-run --include-diff` SHALL include the same comparisons as proposed
rather than applied changes. Invocations without `--include-diff` SHALL NOT add
content hunks to their reports.

#### Scenario: Applying update reports the reviewed content

- **GIVEN** eligible Git and Marketplace updates with changed selected resources
- **WHEN** `update --include-diff` succeeds
- **THEN** it SHALL apply the prepared exact revisions without prompting and report their normalized resource/supporting-file diffs as applied

#### Scenario: Ordinary update does not opt into content disclosure

- **GIVEN** an upstream change containing a distinctive instruction or command in a selected resource
- **WHEN** `update` runs without `--include-diff`
- **THEN** it SHALL retain the ordinary update-report behavior without adding content hunks or disclosing that content through a new diff report

### Requirement: Diffs use complete readable resource views

The comparison SHALL use harness-independent normalized definitions derived from
verified resources and exact admitted supporting-file bytes, not installed files,
generated harness files, or whole registry repositories. It SHALL cover skills,
commands, subagents, hooks, MCP servers, and selected collections.

Normalized definitions SHALL expose instruction bodies as readable multiline
text and preserve every resource field relevant to compilation or selection,
including preserved extension fields and source-layout-specific semantics.
Changes to source-file formatting, comments, source locations, or map-key
ordering alone SHALL NOT produce a normalized-definition content change.
Supporting-file content SHALL be compared without such normalization.

Shared source manifests SHALL contribute only the selected resource's
definition, not unrelated entries. The report SHALL label its definitions as
normalized review content and SHALL NOT promise original authored files or
patches suitable for application.

#### Scenario: All supported resource definitions are reviewable

- **GIVEN** selected skills, commands, subagents, hooks, MCP servers, and collections with changed definitions in Git or Marketplace inputs
- **WHEN** an update diff is prepared
- **THEN** the corresponding bodies, executable declarations, selection fields, and changed semantic metadata SHALL be visible in readable normalized definitions
- **AND** changed preserved extension fields SHALL not silently disappear from the comparison

#### Scenario: Equivalent Git and Marketplace definitions use the same review view

- **GIVEN** equivalent verified resource definitions acquired from Git and a Marketplace resource artifact, differing only in original formatting, source locations, or acquisition metadata
- **WHEN** their normalized review content is rendered
- **THEN** it SHALL be identical without changing either resource's existing revision identity
- **AND** source-only comments or formatting SHALL not be presented as resource-content changes

#### Scenario: Unselected shared-manifest entries are excluded

- **GIVEN** a changed selected MCP server or hook and a distinctive changed unselected entry in the same source manifest
- **WHEN** an update diff is rendered
- **THEN** it SHALL expose the selected definition's change without including the unrelated entry

### Requirement: Diffs describe effective resource changes with provenance

Diffs SHALL compare the old and candidate effective selected-resource sets after
normal selection, collection expansion, excludes, required policy, and collision
resolution. Resource identity in the report SHALL preserve source, scope,
resource kind, installed name, and resource coordinate where applicable, including
distinct same-URL paths/refs and independently selected hooks.

Allowed selected-resource additions/removals SHALL be reported as additions or
removals. A changed collision winner SHALL identify both old and new sources,
including when the reviewed content is identical. The report SHALL NOT infer
renames or relax an existing newly-missing-selection refusal.

Collection definition/membership changes SHALL be visible alongside changes to
their affected selected members. A member still selected independently SHALL
not be reported as removed from the effective installation merely because a
collection stops selecting it.

#### Scenario: Selection and collection changes are distinguished

- **GIVEN** a changed `all: true` or tag-selected set, or a changed selected collection that removes one member and adds another
- **WHEN** the candidate passes the existing selection safeguards
- **THEN** the report SHALL identify the allowed effective additions/removals and collection membership changes
- **AND** a removed collection member that remains directly selected SHALL remain in the effective set rather than be reported as an installation removal

#### Scenario: Changed source and complementary occurrences remain visible

- **GIVEN** a changed collision winner, distinct selected scopes or same-URL paths/refs, or independent same-named hooks
- **WHEN** their candidate changes are reported
- **THEN** the report SHALL retain the distinct occurrences and old/new source identities rather than merging them by resource name alone

### Requirement: File and revision changes have explicit representations

Changed text SHALL use deterministic unified hunks with additions/deletions and
stable resource-relative review paths. Supporting-file additions, deletions,
content changes, and admitted executable-mode changes SHALL be represented.
Binary or non-renderable content SHALL be identified by path, change type,
available old/new byte sizes and digests, and executable-mode changes without
printing its raw body.

A source revision or acquisition-evidence change with identical normalized
definitions, supporting bytes, and admitted executable modes SHALL be labeled
as having unchanged reviewed resource content, not given fabricated hunks.
This statement SHALL NOT imply original authored bytes or generated harness
outputs are identical.

These representations SHALL preserve existing source-containment, symlink,
special-file, verification, and read-limit contracts. They SHALL NOT introduce
newly accepted source shapes or claim to show symlink targets absent from the
verified acquired representation.

#### Scenario: Supporting file changes are not hidden by an unchanged definition

- **GIVEN** an unchanged selected resource definition with added, removed, text-modified, binary-modified, or executable-mode-only supporting files
- **WHEN** its update diff is rendered
- **THEN** every admitted changed supporting file SHALL be identified, textual changes SHALL have hunks, and binary/mode-only changes SHALL have explicit metadata rather than raw binary output

#### Scenario: Evidence-only updates do not invent content changes

- **GIVEN** an updated Git revision or refreshed Marketplace archive evidence with unchanged reviewed resource content
- **WHEN** the update report includes diffs
- **THEN** it SHALL identify the revision/evidence change, state that reviewed resource content is unchanged, and emit no content hunks

#### Scenario: Review does not broaden symlink or special-file admission

- **GIVEN** candidate input rejected by the existing source-containment, supporting-file symlink, or special-file contract
- **WHEN** an update with diffs prepares that input
- **THEN** it SHALL retain the existing refusal and preserve consumer/installation state rather than reading a foreign target or manufacturing a supported diff

### Requirement: Requested comparisons complete before new update publication

When `--include-diff` is requested, all comparisons required for eligible changes
SHALL be acquired, verified, and prepared before publishing new update
configuration or acquisition-lock state. Missing or unverifiable recorded
historical content and comparison-generation failures SHALL exit nonzero and
identify the affected comparison without substituting an empty baseline,
claiming unchanged content, silently omitting a change, or applying the new
updates first.

This requirement SHALL NOT bypass existing authorization or effective revocation
policy to retrieve historical content. Existing authorized recovery of a
pre-existing dependency transaction in an applying update remains separate from
new update publication.

An absent previous writable acquisition lock SHALL be distinguished from a
recorded baseline that cannot be loaded, as specified by `update-preview`.
An applying invocation SHALL consume the exact candidate revisions used for its
comparisons, subject to existing publication-generation checks, rather than
resolving newer candidates after the comparison.

#### Scenario: Unavailable recorded baseline refuses before new updates

- **GIVEN** a recorded historical baseline whose required content cannot be obtained or verified and another eligible update
- **WHEN** `update --include-diff` or `update --dry-run --include-diff` prepares its report with no pending recovery
- **THEN** it SHALL identify the failed comparison and exit nonzero without publishing new config/lock state or changing installed content
- **AND** it SHALL not display a complete-success report, substitute installed files, or silently treat the baseline as empty

#### Scenario: Candidate movement does not change an applying invocation's comparison

- **GIVEN** exact candidates have been prepared and compared, and upstream advances before publication
- **WHEN** that `update --include-diff` invocation applies
- **THEN** its acquisition locks and reported dependency changes SHALL use the compared candidates, or it SHALL retain an existing generation-conflict refusal
- **AND** it SHALL not resolve the newer upstream revision as part of applying the prepared comparison

### Requirement: Reports distinguish dependency publication from materialization

Reports SHALL distinguish proposed changes, published dependency updates,
completed materialization, retained/skipped resources, and preparation or
post-publication failures. If dependency publication succeeds but harness
synchronization fails, the report SHALL identify that phase boundary, retain the
existing recovery/error contract, and SHALL NOT claim that every diff was
successfully materialized or that the dependency update was rolled back.

Output-write failures SHALL return a nonzero exit. A failure writing the result
report after publication SHALL NOT be represented as a pre-publication refusal
or a rollback.

#### Scenario: Post-publication failure is not reported as full application

- **GIVEN** prepared diffs whose dependency update publishes successfully but whose subsequent harness synchronization fails
- **WHEN** `update --include-diff` reports its outcome
- **THEN** it SHALL distinguish published dependency changes from incomplete materialization, exit nonzero, and retain the existing recovery/error behavior
- **AND** it SHALL not label all harness changes as completed or claim unchanged acquisition locks

#### Scenario: Result-output failure does not imply rollback

- **GIVEN** a prepared update that publishes successfully
- **WHEN** writing its requested diff result fails
- **THEN** the command SHALL exit nonzero without claiming publication was prevented or rolled back

### Requirement: Explicit content disclosure is isolated and terminal-safe

New review-content output introduced by this feature SHALL expose reviewed
resource content only in explicitly requested diff output. Existing completed-hook
reviews remain governed by `hook-sync-diagnostics`; a dry-run SHALL NOT report
completed hook mutations.
It SHALL show authored instruction and configuration values without resolving
local environment variables, reading local credentials to populate them, or
inserting acquisition authentication. Source locators and acquisition errors
SHALL retain applicable credential redaction.

Diagnostics, operational logs, and telemetry SHALL NOT contain diff hunks,
instruction bodies, hook commands, supporting-file bodies, or resource
configuration values introduced by this feature. Untrusted names, paths, and
content SHALL NOT execute terminal control sequences; multiline review content
SHALL remain readable. Output ordering SHALL be deterministic for identical
verified inputs.

Help or reference documentation SHALL state that explicit diff output can
disclose sensitive values authored in resources and is not a secret scanner or
security finding.

#### Scenario: Authored values are shown without resolving local secrets

- **GIVEN** changed authored instructions or configuration containing a distinctive literal value and a local environment-variable reference, with a different distinctive value in the invoking environment
- **WHEN** an explicit diff is rendered
- **THEN** it SHALL show the authored change and literal variable reference without expanding the local environment value
- **AND** neither body content nor either value SHALL be added to diagnostics, operational logs, or telemetry

#### Scenario: Terminal controls and locator credentials are not emitted unsafely

- **GIVEN** changed resource text or filenames containing terminal controls and a source locator/acquisition error containing credentials
- **WHEN** a preview or applying update renders its report
- **THEN** it SHALL preserve readable multiline diffs while neutralizing terminal controls and retaining applicable locator/error credential redaction
- **AND** repeated rendering of identical verified inputs SHALL preserve deterministic ordering

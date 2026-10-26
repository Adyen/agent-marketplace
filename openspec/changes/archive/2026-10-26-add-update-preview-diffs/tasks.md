## 1. Prove preview boundaries before implementation

- [x] 1.1 Add isolated command tests for all `update-preview` scenarios, with real v4 selections and newer upstream content; add each scenario's `// spec:` binding and observe new-flag tests fail against the base command.
- [x] 1.2 Add non-vacuous state-preservation fixtures for both materialization modes, existing ownership/backups/inventory, missing roots/locks, pending journals, unsupported roots/includes, cancellation, and acquisition counters; verify that fixtures detect an intentional mutation or acquisition before relying on refusal assertions.
- [x] 1.3 Add preview/apply candidate-parity and targeting/ref-bump controls using fixed local Git and Marketplace fixtures; verify that the existing applying command establishes independently authored expected candidates and scope boundaries.

## 2. Separate update preparation from publication

- [x] 2.1 Expose candidate dependency preparation before commit without changing normal update semantics; verify existing update, staged-lock, initial-acquisition, selection-refusal, and Marketplace partial-update regressions pass.
- [x] 2.2 Reuse prior/candidate resolution with overlays and inventory disabled, preserving complete scope/source identities and selected collection roots; verify independent same-named hooks, complementary scopes, collisions, and collection selectors through actual resolver consumers.
- [x] 2.3 Prepare missing writable acquisition evidence without the mutating bootstrap, retain exact-cache leases, and add the read-only pending-journal path; verify the preservation/refusal tests from section 1 pass and no temporary residue remains.
- [x] 2.4 Consume the prepared exact candidates through the existing dependency transaction and generation checks; verify an upstream-advance test applies only the compared candidates and an input-generation-change test retains the existing refusal.

## 3. Build complete normalized resource review content

- [x] 3.1 Add independently authored failing fixtures for every new definition/provenance/selection scenario in `update-output`, including semantic metadata and extension-only changes for each supported resource kind; add scenario bindings and verify a body-only or name-only comparison fails these controls.
- [x] 3.2 Implement the readable normalized definition view using existing canonical data/rendering where complete, with a deterministic supplemental metadata view where necessary; verify equivalent Git/Marketplace definitions render identically, bodies remain multiline, and existing artifact/source-revision/inspection golden bytes and digests remain unchanged.
- [x] 3.3 Compare effective selected-resource and collection sets without inferring renames or weakening the missing-selection guard; verify additions/removals, independent selection of removed members, changed collision winners, distinct refs/paths/scopes, and additive hooks.
- [x] 3.4 Add exact supporting-file comparison and deterministic unified text hunks plus binary/size/digest/executable-mode metadata; verify supporting-file-only fixtures expose actual changes and existing containment, symlink, special-file, and read-limit refusals remain effective.
- [x] 3.5 Distinguish unchanged reviewed content, evidence-only changes, initial acquisition, unavailable recorded baselines, and live/unlocked sources; verify each outcome uses its intended label and never substitutes installed content or an empty historical baseline.

## 4. Integrate explicit diff output and truthful outcomes

- [x] 4.1 Prepare complete requested comparisons before new publication, retaining authorization/revocation policy and cleaning private temporary storage; verify historical-body, verification, and comparison failures preserve config/locks/installation and do not suppress unrelated missing-pin reporting.
- [x] 4.2 Add `--dry-run` and `--include-diff` CLI wiring, injected-writer rendering, help, and public-command behavioral witnesses; verify all four command combinations, successful preview exits, no-update cases, ordinary update without hunks, and no confirmation prompt.
- [x] 4.3 Preserve dependency-publication/materialization/output phase outcomes in update results; verify real publication followed by sync or writer failure returns nonzero and never claims complete materialization, pre-publication refusal, or rollback incorrectly.
- [x] 4.4 Keep authored content exclusively in requested diff output, preserve literal local-variable references, and reuse terminal/locator sanitization; verify distinctive body/value fixtures appear only in requested stdout, not diagnostics/logs/telemetry, and terminal-control/credential fixtures cannot escape safe rendering.
- [x] 4.5 Preserve all existing `update-output` scenario bindings and add bindings for the modified requirement's preview/partial-diff scenarios; verify missing-resource and missing-collection cases retain existing partial-update semantics.

## 5. Exercise the real CLI and document the boundary

- [x] 5.1 Add successful v4 E2E invocations for both new flags and their combination, with Git and Marketplace resource/collection fixtures; verify public-command coverage witnesses execute successfully rather than relying on help, refusals, or an allowlist.
- [x] 5.2 Exercise preview preservation in both materialization modes and retained source/ownership/refusal behavior through the real binary; verify changed source bytes and successful controls make every no-write assertion non-vacuous.
- [x] 5.3 Update the CLI reference and upstream-change explanation with the command matrix, normalized-review examples, live-source and sync limits, permitted cache writes, content-disclosure warning, and independent invocations; verify examples match behavioral test output.

## 6. Measure, validate, and review the implementation

- [x] 6.1 Capture existing update-planning benchmarks before the preparation refactor and repeat after it; add realistic comparison benchmarks for many files, long lines, and supporting-file-only changes, report allocations and disk/network scope, and review measured costs without weakening safety or silently truncating output.
- [x] 6.2 Run affected package/command tests first, then `make validate`, `make spec-check`, and `make validate-e2e`; verify all required witnesses execute, all active scenario bindings are genuine, and failures are fixed rather than reclassified as coverage.
- [x] 6.3 Review the full intended implementation for consumer-path reachability, failure/publication phases, unintended writes, ownership/scope boundaries, content disclosure, source safety, and non-vacuous regression evidence; verify findings against code, fix actionable issues, and rerun affected checks.

## 7. Finalize the implemented change

- [x] 7.1 Run `openspec archive add-update-preview-diffs --yes` locally, without committing or pushing as requested; verify the archived change and normative specs retain every implemented requirement/scenario and `make spec-check` passes.

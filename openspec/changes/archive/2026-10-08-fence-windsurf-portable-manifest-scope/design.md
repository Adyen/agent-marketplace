## Scope and root cause

Repository Windsurf output planning starts from the repository base while native files belong below `.windsurf/`. Portable manifest nomination currently reuses the planning base without validating the native namespace. A matching digest is sufficient to delete an unrelated file below that base.

## Decision

Reject repository-scoped Windsurf entries at the shared portable manifest loading boundary when their cleaned relative path is not a descendant of `.windsurf/`. Return the existing unsafe-path identity so materialization and scoped command consumers emit `materialize.manifest.error`. Validate before imported claims can authorize target handling, ownership hashing, removal, or manifest publication. Keep existing reporting roots and serialized paths unchanged. A late removal-only check was insufficient: the regression also observed canonical rewriting of the refused manifest, so admission must fail before state is used.

The `.windsurf` root itself is not an eligible nomination. A prefix such as `.windsurf-other/` is not the native namespace. Existing root and scope alias admission remains independently enforced by the destination boundary.

## Compatibility and non-goals

Machine Windsurf output remains rooted in its machine destination, with existing `global_workflows` routing. Other targets retain their current destinations. This is namespace containment, not cryptographic provenance: content-identical nominations within authorized output roots retain their existing ownership contract. SDK synchronization still owns the caller-authorized explicit output directory; its prune semantics are not replaced by Marketplace manifest semantics. The accepted narrow quarantine deletion boundary is unchanged.

## Evidence and verification

The regression independently authors the regular-file digest and manifest; it does not obtain the nominated digest from the production helper. Its positive control first creates and really prunes native Windsurf file-mode output. The unfixed consumer removes the unrelated file with no diagnostic. The fixed consumer must preserve bytes, inode, permissions, modification time, and manifest evidence, refuse with the established diagnostic, and report no removal for the refused entry.

Run affected materialization, SDK and CLI consumers, root/alias controls, strict specification checks, platform checks, and E2E/configuration compatibility. Measure the public Windsurf portable prune path before and after; disk I/O is timed, installation fixture setup is excluded, and there are no network requests.

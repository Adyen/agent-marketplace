## Context

Private exclusive reservation creation alone does not prove a later quarantine rename target is still owned by this attempt. The deferred unlink likewise only has a pathname.

## Decision

Return creator descriptor FileInfo with the reserved name. Represent reservation authority using existing StaleFileCandidate metadata plus independently fixed empty SHA256 digest; shared no-follow/nonblocking bounded descriptor matcher checks inode, regular single-link shape and empty bytes. Require matching reservation after the public quarantine operation boundary before rename. Deferred pre-publication reservation cleanup runs the same match and preserves unknown/replacement generations; conflict and close errors join primary only on failure. Capture failure returns no deletion authority; known captured attempts are eligible for guarded cleanup. Existing successful rename, payload verification/restoration and post-publication removal stay unchanged in this layer.

## Verification

Public SDK sync replaces the actual private reservation after it was created and before rename: parent overwrites/deletes foreign replacement and reports success/red. Guard stale-conflicts, preserves replacement bytes/inode/mode/mtime, original empty reservation retained outside output, selected stale bytes and output membership, and removes only own stage. Healthy retry actually prunes selected stale payload while earlier transaction-prefixed replacement remains untouched. Linux/macOS full SDK/materialization/CLI conversion/repeated races/validate/spec/shellcheck, timed disk prune costs and complete all-file adversarial review required. Final payload-deletion/restoration, removal reporting/empty-parent and admitted-case audit remain separate; no whole transaction/uncooperative atomicity.

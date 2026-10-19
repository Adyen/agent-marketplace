## Decisions

Hook canonical payloads gain an optional `SupportFiles` map of relative path to
digest. The digest binds bytes and executable mode with a versioned domain
separator. Empty declarations are omitted, preserving historical payload bytes.
Only mirror publication populates this field; index-only reconstruction retains
its historical intrinsic artifact identity.

Publication uses the same adjacent support-file selection as Git materialization.
Native aggregate definitions are excluded. Existing bounded, containment-checked
support reads and canonical archive support members are reused. Changes to support
bytes or mode produce a new content revision, never replacement archive bytes.

Mirror consumers verify declared membership and digests before cache writes,
stage support in the archive-keyed cache, and feed explicit declared files into
the existing hook-support materialization and conflict reconciliation path.
Targets with a native support-file component reference its routed installation;
aggregate-only targets reference the verified archive-keyed support cache directly.
Neither case relies on harness plugin-root environment variables.

Legacy mirrored hooks referencing a plugin-root token without delivered assets
fail with `marketplace.hook.support-missing`. A publisher refresh and explicit
installation of the new revision are required; no pinned artifact is rewritten,
and no external command is executed to validate it.

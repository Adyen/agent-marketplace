## Context

PR #970 supports arbitrary absolute Claude Code machine roots. Its generic
marker walk still treated every descendant JSON file as a cleanup candidate.
CodeRabbit and an independent regression confirmed that another account's or
repository's owned entries could be removed. Expanding a list of inactive
account fences would never cover every unrelated path.

Native hook support also re-ran standalone portable-directory checks and a
strict transaction symlink snapshot after the destination boundary had admitted
a root alias. These checks were suitable for unbound callers, not for an
explicitly authorized machine-root alias.

## Decisions

- Derive eligible relocated cleanup files from the existing harness aggregate
  declaration. Defer empty component-plan marker cleanup to that same bounded
  pass. Keep directory coverage, protected aggregate paths, deterministic
  order, real stale-entry pruning, and default/unset behavior.
- Do not walk unrelated directories or add a second target-path registry.
  The declaration already has compiler-backed coverage tests.
- Carry the admitted boundary into marker cleanup and reuse captured regular-file
  reads and generation-checked publication. Direct declared-file lookup must not
  follow a component retargeted after initial admission; shared modes survive.
- Reuse destination-bound directory capture and the existing descriptor-relative
  managed-link observer for native hook support. Standalone verification keeps
  its strict transaction snapshot behavior, and source bytes, permissions and
  repeated link checks remain required before native publication.
- Compare canonical caseless Unicode identities for overlap. Lowercasing alone
  misses case variants such as final sigma and long S when no existing object
  can establish their identity; use the existing text dependency's case folder.

## Verification

Both regression tests failed before the fixes. Test symlink and file modes,
actual stale resource/aggregate cleanup, unrelated file bytes/inode/mode/mtime,
native hook and MCP publication through an admitted alias, and a clean dry run.
Run native hook/security regressions, affected validation and spec gates, then
repeat host-driven adversarial review and obtain fresh bot reviews.

# Registry containment

A registry controls its own bytes. `agent-marketplace` reads those bytes on
behalf of the invoking user and writes them into directories an AI harness reads
back — the skill cache under `~/.cache/agent-marketplace`, and generated output
such as `.claude/skills/` or `.factory/hooks/`. Anything a registry can make this
tool read, it can therefore publish somewhere a harness will happily consume.

Containment is the rule that closes that gap: **every byte read from a registry
must come from inside that registry's root, and must come from a regular file.**

## Where it is enforced

`internal/registryfs` is the only way registry content is read. It wraps
[`os.Root`](https://pkg.go.dev/os#Root), so each path component is opened
relative to the previous directory descriptor and a component that resolves
outside the root fails inside the same syscall sequence that performs the read.

```
catalog.Load / catalog.Discover ─┐
collection.DiscoverCollections  ─┤
skill.DeclaredNameFromFile      ─┼─► internal/registryfs (os.Root) ─► registry tree
tui.ReadSkillMarkdown           ─┤
materialize.sourceTree          ─┘
```

Provenance travels with the data so the confinement root is never guessed:
`catalog.Entry.SourceRoot` is stamped at discovery, rides `resolver`'s
`ResolvedArtifact` into materialization, and becomes the root of the
`registryfs.FS` that reads support files. When a resource arrives without a
`SourceRoot` (an in-memory fixture, a synthetic entry), the resource's own
directory becomes the root: narrower, never wider.

### Point of use, not check-then-use

Validating a path and then reopening it by name is a time-of-check/time-of-use
bug: a registry is a directory tree that `git pull` rewrites in place, and a
tree that validated cleanly a moment ago can contain a symlink now. Because
`registryfs` resolves through descriptors at the moment of the read, a swap
between the check and the read changes nothing — the read fails.

For the same reason **no verdict is cached across calls.** `ValidateTree` is an
eager diagnostic pass that produces a good error message before ingestion starts;
it is not the mechanism, and its result is deliberately not remembered.

## The rules

| Rule | Local registry | Remote (cloned) registry |
|---|---|---|
| Reads confined to the registry root | yes | yes |
| Regular files and directories only | yes | yes |
| Relative symlinks that stay inside the root | allowed | refused |
| Absolute symlinks | refused | refused |
| Symlink pointing outside the root | refused | refused |

**Local registries may use symlinks** as long as they resolve inside the root. A
developer registry that shares one `scripts/` directory across several skills is
legitimate; reading `~/.ssh/id_rsa` through a skill is not.

**Remote registries refuse symlinks outright.** Nothing in a tree this tool
cloned itself needs one, and refusing keeps the cache comparable with the
upstream commit. The rejection is reported with the offending path so the
registry author can fix it.

**Absolute symlinks are refused everywhere, even when they point back inside the
root.** An absolute target cannot be followed from a directory descriptor;
honouring it would mean resolving a path by name again, which is exactly the
check-then-use pattern above. Registry symlinks must be relative.

**Only regular files and directories are readable.** The type is taken from the
descriptor that will be read (`fstat`), not from an earlier lookup by name. On
Unix that descriptor is opened non-blocking, because a plain `open(2)` of a FIFO
with no writer blocks forever: a single named pipe committed to a registry could
otherwise wedge every `list`, `status`, and `sync` until the process was killed.

**`.git` is skipped by the eager validation walk.** It is a clone artifact this
tool created, it is never ingested, and walking it makes every refresh pay for
the whole object store (measured: it dominated validation cost, ~7 MB and ~60k
allocations per walk of a 100-skill registry). Skipping it narrows the walk to
registry content; content reads are still confined regardless of path.

## What happens when a read is refused

Severity follows blast radius.

- **Skill support tree → the sync fails.** The cache directory is read by the
  harness as one unit, so a partially copied skill is worse than a refused one.
  The error names the path and the reason.
- **Hook support directory → the file is skipped** with a
  `materialize.registry-source-refused` warning, and the hook still installs.
  Hook helpers are individual files; one refused entry beside a legitimate
  `keep.sh` should not disable the hook.
- **Discovery/ingestion → the registry is rejected.** Ingestion turns read
  failures into diagnostics and keeps going, so `registryfs.FS.ValidationError`
  records the first containment or file-type rejection and the catalog loader
  fails closed on it rather than returning a half-read registry.

Every rejection is also logged through `slog` (`registry root rejected`,
`registry support file refused`, ...) with the path and the redacted registry
URL, so an operator can find the offending registry in the logs of a run whose
diagnostics were suppressed.

## Testing the policy

Exploit regressions live next to the code they defend:

- `internal/registryfs/registryfs_test.go` — escape, absolute-symlink, remote
  symlink, special-file, post-validation swap, and concurrent-rewrite cases.
- `internal/catalog/registry_security_test.go` — discovery-side rejections,
  including a FIFO manifest that must not hang.
- `internal/materialize/registry_containment_test.go` — end-to-end: an external
  sentinel file reached through a registry symlink must never appear anywhere
  under the repo or `HOME` after a sync, in symlink mode, in file mode, or in
  `--check`.

The materialization tests assert the *absence of the sentinel bytes* rather than
just a failed copy, so any future code path that reads a registry file by name
again fails them.

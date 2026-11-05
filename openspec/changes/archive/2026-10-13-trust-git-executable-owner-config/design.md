## Context

`gitAuthConfigReader` validates every opened configuration file: regular, singly linked, not group/other writable, owned by root or the effective user. Acquisition runs `git` through `exec`'s `PATH` lookup. An install-user Homebrew prefix (`$(brew --prefix)`) is owned by that account, including `etc/gitconfig` and the executable in its Cellar.

## Decisions

1. **Authority, not names.** Trust is extended only to an account that already controls acquisition: the owner of the executable we run can rewrite it. An organisation setting or a username allowlist would add configuration surface without adding safety.
2. **Resolve like `exec`.** Use `exec.LookPath("git")` and follow symlinks to the executed file (`bin/git` → `Cellar/git/<version>/bin/git`). The final file's owner is taken; symlink and directory owners are not added.
3. **Protected executable only.** Group- or other-writable executables, unresolvable paths, non-regular files and relative lookups extend no trust. A wider group never becomes trusted through the executable, and a relative `PATH` entry (which later commands resolve from other working directories) cannot make the trusted owner differ from the executed program.
4. **Uniform owner set.** The same owner set (root, effective user, executable owner) applies to system, global and included files; every other check is unchanged.
5. **Accepted boundary.** Ownership is treated as authority over the executable; a read-only mount that would deny its owner that authority is not distinguished.

## Verification

Tests cannot create files owned by another account, so the effective UID is substituted. Test-created files then belong to "another" account, and the authenticated TLS fixture is driven through a Homebrew-style symlinked `git` wrapper on `PATH`.
- **Trusted case:** the configuration owned by the wrapper's owner authenticates; this failed before the change.
- **Refused cases:** group-writable and other-writable wrappers, and a root-owned `git` with a configuration owned by another account, are refused before the registry is contacted.
- **Real install:** a manual check with a Homebrew installation owned by a dedicated install user lists a local HTTP registry without disabling the system configuration.

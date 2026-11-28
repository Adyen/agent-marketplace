// Package fsboundary provides the shared Unix descriptor-bound filesystem
// primitives behind the agent-marketplace publication and read boundaries.
//
// Authority is bound to opened directory descriptors, never to lexical
// paths or pre-open Stat results: a captured [Dir] keeps its directory file
// descriptor open, and every leaf open, publication, and removal executes
// relative to that descriptor with openat(2)/renameat(2)/unlinkat(2). A
// parent directory that is renamed or replaced after capture therefore
// cannot redirect an operation into a replacement tree; the operation stays
// bound to the captured object.
//
// Leaf objects are validated through the opened descriptor: no-follow and
// nonblocking opens, regular-file and single-link-count checks, bounded
// limit+1 reads, and a post-read identity check that turns in-place growth
// or mutation into [ErrGenerationConflict]. Destinations are captured once
// with an explicit link policy ([LinkPolicyUser] or
// [LinkPolicyRepository]); publication re-verifies the captured generation
// immediately before the descriptor-relative rename and reports
// [ErrGenerationConflict] for replacements observed by that validation.
//
// Exclusion across a capture/publish window is the caller's responsibility.
// Domain locks coordinate cooperating writers; they do not exclude processes
// that ignore the lock. Descriptor-relative operations contain ancestor
// redirection but validation and the final rename or unlink are not an atomic
// namespace compare-and-swap against uncooperative directory-entry writers.
//
// Pre-mutation refusal preserves user bytes, modes, mtimes, and links.
// Directory-sync failures after a successful rename or unlink report
// ErrDurabilityUncertain: namespace mutation occurred, but durable persistence
// is uncertain. Callers must inspect or recover that state, not assume refusal.
//
// # Platforms
//
// macOS and Linux bind to descriptors with openat(2), renameat(2), and
// unlinkat(2). Windows binds to directory handles: every open is NtCreateFile
// relative to a captured handle, one component at a time, with
// FILE_OPEN_REPARSE_POINT so that a reparse point is opened as itself and then
// refused. Both implementations expose the same API and the same refusal
// identities; the shared logic lives in the portable files and the platform
// primitives live in os_unix.go and os_windows.go.
//
// Windows differs in these ways, all fail-closed:
//
//   - Symlinks, junctions, mount points, and every other reparse point are
//     refused as [ErrUnsafePath]. The link policies that honor links on Unix
//     refuse on Windows.
//   - Components that Windows treats specially (device names, alternate data
//     stream syntax, trailing dot or space) are refused as [ErrUnsafePath].
//   - A directory cannot be flushed, so [Dir.Sync] succeeds without flushing.
//   - [FileInfoIdentity] cannot see a file ID, change time, or link count,
//     because a Windows os.FileInfo does not carry them; it is weaker than an
//     identity taken from a handle and the two are not comparable.
//   - [Dir.NativeRoot] opens the captured directory by its final path and
//     verifies the captured identity; a replacement is detected, not prevented.
package fsboundary

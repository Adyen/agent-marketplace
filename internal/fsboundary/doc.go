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
// Supported platforms are macOS and Linux only.
package fsboundary

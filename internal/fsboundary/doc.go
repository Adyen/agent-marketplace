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
// [ErrGenerationConflict] on any replacement.
//
// Exclusion across a capture/publish window is the caller's domain lock
// (for example the dependency-transaction path locks or the ownership
// flock); these primitives make every check descriptor-relative so that
// holding such a lock is sufficient.
//
// Refusal preserves state: a failed capture, read, precondition, or
// publication changes no user bytes, modes, mtimes, or links, and removes
// only residue created by the failed attempt itself.
//
// Supported platforms are macOS and Linux only.
package fsboundary

## Context

The kernel lock is per opened description/inode. Metadata is diagnostics-only but truncates the held descriptor; a hardlink bypasses final O_NOFOLLOW. Existing tests intentionally support selected state-root and shared lock-directory aliases, so they must not be banned indiscriminately.

## Decisions

Reuse shared FileInfoIdentity with native nonblocking/no-follow/close-on-exec open flags. Validate the opened regular/single-link shape before kernel acquisition, after a contended wait succeeds, and immediately before diagnostic rewind/truncate. Initial/acquisition refusal returns typed unsafe shape and closes the descriptor. A late shape change skips diagnostics at their existing best-effort debug boundary, preserving the real held kernel lock and release. No stale-lock policy, timeout/cancel/reentrancy changes, alias ban or new dependency. Source comments unchanged. Namespace capture is a separate following slice.

## Verification

Actual public LockContext hardlinked path accepted and rewrote foreign bytes on parent/red; now refuses typed unsafe file, leaves no session and preserves bytes/green. Removing the alias lets real regular acquisition write the actual current pid diagnostic and release. Bounded child FIFO refuses and preserves shape/object. Link added by real metadata producer after acquisition preserves bytes and still releases. Full Linux/macOS ownership, count20 lock/race/alias compatibility, validate/lint/build/spec and complete file review before push. Existing full acquire/diagnostics/release disk benchmark, 2sx5/no95CI:59.612→64.507µs,1144→1769B,16→19alloc. This is safety cost, not a speedup claim.

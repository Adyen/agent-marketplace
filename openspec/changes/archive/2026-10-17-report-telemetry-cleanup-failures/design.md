## Decisions

Use the existing filesystem operations and wrap their errors with `%w`.
Diagnostic completion prints its cleanup failure and retained payload path,
then returns the filesystem cause instead of printing success. Its existing
command-lifecycle caller remains best effort, so telemetry does not change
the investigated command's outcome.

Installation-ID reset retains its current operation order: remove the old
ID, clear pending telemetry records, then clear diagnostic evidence. A
diagnostic cleanup failure returns an error before the CLI creates a
replacement ID or prints reset success. This is not an atomic reset or a
rollback guarantee; retry after repairing permissions finishes cleanup.

Failure fixtures restrict diagnostic-directory write permission on the
supported Unix platforms. They use isolated temporary roots and restore
permissions during cleanup. As with existing permission tests, they require
a non-root process rather than adding filesystem injection hooks.

The diagnostic test observes actual accepted HTTP delivery before making
the review directory read-only and compares retained payload bytes. The
reset test exercises the public command, verifies preserved evidence and
metadata, pins already-completed partial cleanup, and witnesses a successful
retry. Existing successful diagnostic completion remains the positive control.

## Validation

Witness both cleanup tests failing before the production fix and passing
afterward. Run affected packages, validation, strict specification checks,
and telemetry E2E coverage. Review filesystem causes, partial effects,
best-effort diagnostic callers, and every joined lock error before pushing.

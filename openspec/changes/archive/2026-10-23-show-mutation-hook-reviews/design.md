## Context

See proposal.md for motivation. Sync already returns a materialization report and promoted warning diagnostics. Silent mutation sync discards the report; install additionally filters most warnings, and update discards collected warnings. Production TUI sync and update adapters also return only warnings and errors.

## Goals / Non-Goals

Preserve the existing report from the actual publishing run through every mutation entry point. Reuse the existing hook formatter for CLI and TUI output. Add no new acquisition, filesystem scans, hook parsing, or approval step. Frontmatter control review and general analyzer reuse remain separate work.

## Decisions

- Return the existing SyncResult from silent sync rather than create a second outcome model. Expose its report on mutation results; keep the existing install warning filtering for prerequisites and collisions while retaining materialization warnings.
- Extract the existing hook formatter into a small shared presentation package. Formatting remains outside materialization policy and uses the same ordering, escaping, and before/after payloads for both interfaces.
- Retain completed TUI hook rows separately from snapshot drift. Reuse the existing diagnostics viewport, open it after a hook-changing operation, and replace its review on the next completed materialization. Preserve operation warnings across reload.
- Return available reports with materialization failures so partial successful writes remain reviewable. Preflight failures keep their existing refusal behavior and do not create review rows.

## Risks / Trade-offs

- Multiple install paths can bypass normal named-resource helpers → Exercise ad-hoc, collection, and prepared Marketplace commits.
- Snapshot reload can erase one-time review warnings → Keep them as operation diagnostics and test reopening the viewport.
- Review payloads contain untrusted text → Reuse quoted payload rendering and test terminal control escaping; do not copy payloads into ordinary diagnostics or logs.
- Existing ownership and lock transactions must remain unchanged → Retain their execution order and only carry their resulting report.

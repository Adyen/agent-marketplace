## Context

See proposal.md for motivation and specs/registry-symlink-containment/spec.md for the observable boundary. Exact Git acquisition verifies commit identity and the tracked tree; source revision building separately resolves a configured root and walks its contents before ingestion. The previous snapshot-wide symlink-resolution walk ran before the configured root was known.

## Goals / Non-Goals

**Goals:**

- Keep exact tracked-tree verification independent of whether links resolve locally.
- Apply containment validation to the same configured root used for source revision creation.
- Preserve the existing path-escape error identity while reporting the link and actionable remediation.

**Non-Goals:**

- Following symlinks into unrelated portions of the registry repository.
- Changing source revision digests, acquisition lock schema, or how Git verifies tracked bytes.

## Decisions

- Remove the duplicate snapshot-wide symlink-resolution walk while retaining exact Git commit and tracked-tree checks. Alternative: ignore only dangling links during acquisition; that would still reject unrelated escaping links and leave two competing validation policies.
- Validate symlinks after canonicalizing the configured root, checking each resolved link against that root. Alternative: accept links anywhere in the repository; that would widen a configured root and expose unintended content.
- Format a path-escape error using the link's path relative to the registry snapshot, its literal symlink target, the configured root, and remediation. Alternative: report an absolute cache path; cache paths are not useful for identifying a file in the registry repository.

## Risks / Trade-offs

- [An unsafe link outside one configured root might be selected by another entry] -> Each source revision validates its own configured root and rejects the link when it is in scope.
- [Removing a resolution walk could conceal tampering in a cached snapshot] -> Retain exact tracked-tree verification and test that altered tracked bytes still fail closed.

## Migration Plan

No data migration is needed. A rollback restores snapshot-wide validation, which would again reject unrelated dangling or escaping links.

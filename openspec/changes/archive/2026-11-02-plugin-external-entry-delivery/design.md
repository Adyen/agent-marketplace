## Context

Lock building already acquires each selected external catalog entry as its
own exact Git registry record with a plugin-v1 root. The root's manifest
binds the owning catalog entry, the entry's overlay digest, and the
post-exclusion member keys. The delivery seam still treats every entry with
a Git source as pending, because the resolved plugin artifact carries only
the entry's source requirement, not a decoded package or package root.

## Goals / Non-Goals

**Goals:**

- Give a resolved external entry the same inputs an in-path plugin has at
  delivery: a decoded package, a package root inside an exact snapshot, and
  the planned member keys.
- Reuse the existing snapshot leasing, plugin decode, delivery resolution,
  and package generation code without a second decode policy.

**Non-Goals:**

- Changing lock records, acquisition at lock time, or catalog resolution.
- Changing native delivery policy or registration.

## Decisions

### D1: Deliver from the lock record, never the entry ref

The delivery input for an external entry is the locked registry record
whose single plugin-v1 root names that entry's origin and subdirectory.
Delivery opens that record's commit through the same exact-snapshot path
other locked roots use (read-only lease in locked, check, and offline
modes). The entry's ref is never resolved at delivery. Alternative
considered: resolving the ref during delivery, rejected because delivery
could then differ from the lock.

### D2: Decode with the lock-time policy

The package decodes with the catalog's plugin format and the entry's
strict and overlay inputs, the same inputs the lock manifest binds, so the
decoded members match the committed member keys. Alternative considered:
decoding without the overlay, rejected because the member set could differ
from the lock.

### D3: Member set from the committed keys

Delivered members are the post-exclusion member keys the external record
commits, joined with the plan's authored exclusions, exactly as in-path
plugins use their format root's keys.

### D4: Unavailable records block the instance

A missing record or an unopenable snapshot becomes an error-severity
diagnostic for that plugin. The instance is dropped from delivery, so the
existing instance participant keeps its previous projection. Other
artifacts continue.

## Risks / Trade-offs

- [An offline host without the exact snapshot cannot refresh an external
  plugin] → The instance blocks with an error, and its committed projection
  and generation stay usable.
- [Two layers selecting the same external origin] → Lock building already
  refuses conflicting shared origins, so delivery sees one record per
  origin.

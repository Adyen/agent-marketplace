## 1. Design proposal

- [ ] 1.1 Record the selected staging and publication units, staging authorization outside target roots, cross-filesystem behavior, hot-reload observation limits and package/dependency partial visibility.
- [ ] 1.2 Record the interruption state-transition table with reused journal and generation seams and exact read-only and mutating recovery outcomes, including unexpected filesystem generations.
- [ ] 1.3 Record root-bound ownership, status and retirement, including environment/root changes and destination symlink replacement.
- [ ] 1.4 Record whole-set conflict planning, including installed projections, new conflicting packages, unowned byte-identical collisions, prefix/file-directory and case collisions.
- [ ] 1.5 Record edited-owned-file and failed-verification behavior with a future witness per transition.

## 2. Validation

- [ ] 2.1 Check the design against source evidence and run git diff --check.
- [ ] 2.2 Reconcile design references for archive handoff; independent reviews and archive are later-role gates.

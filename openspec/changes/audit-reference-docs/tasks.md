## 1. Reference audit

- [ ] 1.1 Audit every reference page's quadrant and reader against writing-tech-docs; retain the per-page audit outside the repository.
- [ ] 1.2 Verify facts, defaults, constraints, failure cases, and examples against governing specifications and current source.
- [ ] 1.3 Tighten reference prose and link to existing guides; fix navigation and inbound links only when needed.
- [ ] 1.4 Capture practical documented commands safely with go run and record unrun examples with their limitations.

## 2. Verification and delivery

- [ ] 2.1 Pass the frozen-lockfile site install and build without dead links.
- [ ] 2.2 Pass TestDocsContract and the scoped repository validation and strict OpenSpec checks.
- [ ] 2.3 Reconcile the complete published reference claims and apply the deletion test to the candidate diff.
- [ ] 2.4 Archive with openspec archive audit-reference-docs --yes --skip-specs.

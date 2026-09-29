## 1. Exact snapshot boundary

- [x] 1.1 Verify exact snapshot acquisition retains tracked-tree checks without snapshot-wide symlink resolution; annotate `TestExactGitAcquiresSnapshotWithEscapingOrDanglingSymlink` in `internal/cache/exact_git_test.go` with `// spec: registry-symlink-containment/snapshot-contains-unselected-unsafe-symlinks` and run the focused test.
- [x] 1.2 Annotate the read-only tampered-snapshot test `TestExactGitRejectsMismatchAndTampering` with `// spec: registry-symlink-containment/snapshot-tracked-tree-is-altered`; run the focused test to confirm the untrusted error remains observable.

## 2. Source revision boundary

- [x] 2.1 Verify configured-root symlink containment; annotate the narrowed-root subtest in `internal/sourcerevision/builder_test.go` with `// spec: registry-symlink-containment/narrowed-source-root-excludes-unsafe-symlink` and run its focused test.
- [x] 2.2 Prove dangling and escaping links within the configured root both retain path-escape identity and include registry-relative path, authored target, root, and remediation; annotate the covering test with `// spec: registry-symlink-containment/selected-unsafe-symlink-identifies-remediation` and run focused tests.

## 3. Verify and archive

- [x] 3.1 Run `go test ./internal/cache ./internal/sourcerevision`, `openspec validate scope-registry-symlink-validation --strict`, and `git diff --check`; verify every spec scenario has a same-PR test annotation.
- [x] 3.2 Run `openspec archive scope-registry-symlink-validation --yes` in this pull request after the annotated tests and validation pass; verify the archived spec contains the four scenarios.

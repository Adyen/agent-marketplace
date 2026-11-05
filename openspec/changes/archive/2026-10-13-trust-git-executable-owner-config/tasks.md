## Implementation

- [x] Add a failing authenticated-acquisition regression for configuration owned by the `git` executable's owner, with writable-executable and unrelated-owner controls.
- [x] Trust the owner of the protected, symlink-resolved `git` executable alongside root and the effective user.
- [x] Document the owner rule in the private registry guide and environment reference.
- [x] Verify scoped tests, lint, build, strict specs and the combined E2E gate.
- [x] Adversarially review every changed entry.

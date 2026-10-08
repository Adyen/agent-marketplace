## 1. Regression evidence

- [x] 1.1 Add and annotate a differential include-path test against native Git and observe its pre-fix failure.

## 2. Implementation

- [x] 2.1 Join relative and `~/` include paths without lexical cleaning.
- [x] 2.2 Expand `%(prefix)/` include paths through the installed Git.
- [x] 2.3 Document the `%(prefix)/` form.

## 3. Verification

- [x] 3.1 Run focused authentication tests, race checks, scoped validation, and strict spec validation.
- [x] 3.2 Review the intended changes adversarially and address actionable findings.
- [x] 3.3 Run `openspec archive resolve-git-include-paths-like-git --yes`.

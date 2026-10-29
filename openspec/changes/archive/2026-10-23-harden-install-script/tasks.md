## 1. Install script behavior

- [x] 1.1 Add offline tests in `scripts/install_script_test.go` for the install-directory, verification-order, shadow-warning, and temp-directory scenarios, each annotated with its `// spec: install-script/...` line, and see them fail
- [x] 1.2 Resolve the install directory before any request, add the shared `download` helper, fetch SHA256SUMS before the binary, hash from stdin, and add the shadow warning in `site/public/install.sh`
- [x] 1.3 Extend the fake `curl` and failure cases to cover each failure branch, and confirm the suite passes with `shellcheck` clean

## 2. Website and docs

- [x] 2.1 Reduce the public-site install test to the byte-identical check, and restore the quick start, README, and landing command to `go install`
- [x] 2.2 Move the install-script section of `site/docs/install.md` after the other install methods, and add the authenticity note

## 3. CI plan

- [x] 3.1 Map `site/public/install.sh` to the `go_test` job in `tools/ci-plan` with a matching row in its tests

## 4. Archive

- [x] 4.1 `openspec archive harden-install-script --yes`

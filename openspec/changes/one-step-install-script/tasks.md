## 1. Release checksums

- [ ] 1.1 Add tests in `scripts/` proving `make release` writes `dist/SHA256SUMS` for exactly the four binaries and that `sha256sum --check` passes, annotated with the release checksum scenario
- [ ] 1.2 Generate `dist/SHA256SUMS` in the `release` make target and verify the test passes and Homebrew workflow tests still pass

## 2. Install script

- [ ] 2.1 Add offline Go tests in `scripts/` that run `site/public/install.sh` with fake `curl` and `uname`, one `// spec: install-script/...` annotation per platform, version, verification, and install scenario, and see them fail
- [ ] 2.2 Implement `site/public/install.sh` and verify the tests pass and `shellcheck` reports nothing

## 3. Website and docs

- [ ] 3.1 Add a public-site test asserting the built `install.sh` is byte-identical to the source and the landing page shows the curl command, annotated with the site scenario
- [ ] 3.2 Set `INSTALL_COMMAND`, and lead README Install and `site/docs/install.md` with the one-step command, keeping manual, Go, and source installs as further options; verify `pnpm run test:public` and `pnpm run test:docs` pass

## 4. Archive

- [ ] 4.1 `openspec archive one-step-install-script --yes`

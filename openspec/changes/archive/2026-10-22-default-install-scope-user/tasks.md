## 1. Tests first

- [ ] 1.1 Add command tests for install without `--scope` writing user configuration and for `install --from ./<relative-dir>` without `--scope` being refused
- [ ] 1.2 Flip the install scope matrix default case to user scope
- [ ] 1.3 Confirm the new and flipped tests fail before the implementation change

## 2. Implementation

- [ ] 2.1 Make the `install --scope` flag default to `user` and update its help text, keeping `ScopeSpecified` from `Changed("scope")`
- [ ] 2.2 Correct the `remove --scope` help text without changing remove inference
- [ ] 2.3 Move the Landing default to user scope and change its example command to `--scope user`
- [ ] 2.4 Update the Landing Playwright assertion to the user default

## 3. Existing fixtures and documentation

- [ ] 3.1 Add `--scope repo` to command tests, E2E fixtures, TUI QA journeys, and web tests that relied on the repository default while preserving their assertions
- [ ] 3.2 Update CLI reference, user journeys, how-to guides, README, and agent skills so install examples and prose state the user default and use `--scope repo` for repository sharing

## 4. Verification

- [ ] 4.1 Run `make spec-check`
- [ ] 4.2 Run `go test ./cmd/agent-marketplace -count=1`
- [ ] 4.3 Run `make test TEST_PACKAGES=./internal/pipeline`
- [ ] 4.4 Run `make validate-e2e`
- [ ] 4.5 Run `make tui-qa` if any journey changed
- [ ] 4.6 Run the Landing Playwright scope test, or record why it cannot run locally
- [ ] 4.7 Run `make validate` once before handoff

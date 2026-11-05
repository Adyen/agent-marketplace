# Tasks

## 1. Recoverable source addition

- [x] 1.1 Add a failing Playwright test bound to `gui-configuration/nonexistent-directory-source-is-refused-in-the-form`
- [x] 1.2 Refuse the source inside the Add source section without writing configuration and keep the workspace usable

## 2. Install preparation failure

- [x] 2.1 Probe the "Resource action could not be prepared" install with a Git registry fixture and record the result
- [x] 2.2 Record the follow-up for the unreproduced failure

## 3. Consistent resource opening

- [x] 3.1 Add a test bound to `gui-shell/enter-and-open-resource-agree-across-source-kinds` covering Marketplace, directory, and Git skill rows

## 4. Search-independent discovery health

- [x] 4.1 Add a failing test bound to `gui-shell/discovery-health-ignores-the-resource-search`
- [x] 4.2 Show unfiltered discovery health in Diagnostics

## 5. Post-install state

- [x] 5.1 Compare the post-install state across directory, Git, and Marketplace sources and record the root cause
- [x] 5.2 Record the decision follow-up because consistency needs a product choice

## 6. Settings tab keyboard focus

- [x] 6.1 Move focus between Settings tabs with arrow, Home, and End keys without an unhandled error

## 7. Archive

- [ ] 7.1 `openspec archive fix-gui-functional-feedback --yes`

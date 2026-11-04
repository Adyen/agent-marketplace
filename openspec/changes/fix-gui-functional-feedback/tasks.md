# Tasks

## 1. Prompt completion notification

- [ ] 1.1 Add a failing test bound to `gui-local-api/completion-notification-renders-without-further-action`
- [ ] 1.2 Fix the root cause so the completion notification renders without further events or actions

## 2. Recoverable source addition

- [ ] 2.1 Add a failing Playwright test bound to `gui-configuration/nonexistent-directory-source-is-refused-in-the-form`
- [ ] 2.2 Refuse the source inside the Add source section without writing configuration and keep the workspace usable

## 3. Install preparation failure

- [ ] 3.1 Reproduce the "Resource action could not be prepared" install with a fixture and record the root cause
- [ ] 3.2 Fix it so the install completes or names the cause and next step, or file a follow-up when it cannot be reproduced

## 4. Consistent resource opening

- [ ] 4.1 Add a failing test bound to `gui-shell/enter-and-open-resource-agree-across-source-kinds` covering Marketplace, directory, and Git skill rows
- [ ] 4.2 Make Enter and Open resource use the same availability and open the inspector for every inspectable row

## 5. Search-independent discovery health

- [ ] 5.1 Add a failing test bound to `gui-shell/discovery-health-ignores-the-resource-search`
- [ ] 5.2 Show configuration-wide discovery health in Diagnostics

## 6. Post-install state

- [ ] 6.1 Compare the post-install state across directory, Git, and Marketplace sources and record the root cause
- [ ] 6.2 Make it consistent, or file a decision follow-up when consistency needs a product choice

## 7. Archive

- [ ] 7.1 `openspec archive fix-gui-functional-feedback --yes`

# Tasks

## 1. Native app name

- [ ] 1.1 Add a failing Go test bound to `gui-distribution/packaged-app-and-unbundled-binary-are-named-agent-marketplace`
- [ ] 1.2 Embed the application name in the macOS executable and the packaged bundle metadata

## 2. Resource list

- [ ] 2.1 Add a failing Playwright test bound to `gui-shell/long-load-failure-warning-stays-inside-its-row`
- [ ] 2.2 Truncate row warnings inside the row
- [ ] 2.3 Add failing tests bound to `gui-shell/clicking-a-row-unfolds-a-folded-inspector` and `gui-shell/clicking-a-row-keeps-an-open-inspector-open`
- [ ] 2.4 Unfold the inspector on row click
- [ ] 2.5 Add a failing test bound to `gui-shell/disabled-and-enabled-rows-use-distinct-indicator-roles`
- [ ] 2.6 Render the disabled row indicator in the warning role

## 3. Sidebar sources

- [ ] 3.1 Add failing tests bound to `gui-shell/named-marketplace-source-shows-its-name` and `gui-shell/generic-or-missing-name-falls-back-to-the-host`
- [ ] 3.2 Expose the configured source name through the local API and label Marketplace sources by it
- [ ] 3.3 Add a failing Playwright test bound to `gui-shell/source-tooltip-is-readable-in-both-themes`
- [ ] 3.4 Replace the native source tooltip with a readable themed tooltip

## 4. Add-source errors

- [ ] 4.1 Add failing tests bound to `gui-configuration/add-source-request-error-renders-in-the-section` and `gui-configuration/add-source-error-clears-after-a-successful-add`
- [ ] 4.2 Render add-source errors inside the Add source section

## 5. Discovery health copy

- [ ] 5.1 Add a failing test bound to `gui-shell/discovery-health-uses-plain-wording`
- [ ] 5.2 Replace the discovery health wording

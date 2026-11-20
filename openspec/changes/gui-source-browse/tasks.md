## 1. Integration preflight

- [x] 1.1 Before editing production code, verify the actual execution target includes the merged shared Marketplace paging/cache implementation and the desktop GUI, and determine a passing baseline for the applicable gates. A closed upstream ticket or movement of the queued base alone is not proof. If the integrated target is missing or conflicts, pause and report; do not rebase, merge prerequisites, change targets, or fork the pager as part of this child.

## 2. Desktop behavior and witnesses

- [x] 2.1 Prove source identity isolation and no redacted data disclosure in local API tests, then implement Marketplace endpoint association while retaining the existing row grouping and Git/directory identity.
- [x] 2.2 Extend built-GUI source and keyboard coverage for source selection, clear with and without a query, no matches, Manage source (including a folded group), and unchanged Add source focus. Bind each modified and added source scenario and update the existing Settings reveal witness to use Manage source.
- [x] 2.3 Extend built-GUI discovery coverage and its local API fixture for delayed later pages, search and clear, a later-page failure, stale responses and cancellation. Bind each new discovery scenario. Confirm the new witnesses fail for their intended reason on the unmodified baseline.
- [x] 2.4 Implement in-place browsing and progressive discovery using the common Marketplace implementation; preserve existing snapshot and discovery health witnesses. Update the GUI design guide and rebuild the desktop frontend bundle.

## 3. Verify and archive

- [ ] 3.1 Run local API tests, the GUI check and built-GUI Playwright suites, the repository validation and spec gates, and capture matched desktop evidence for the source and later-page journeys. Report unrelated baseline failures separately.
- [ ] 3.2 Run `openspec archive gui-source-browse --yes` as the final commit of the implementation PR.

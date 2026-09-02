# TUI Marketplace discovery

## Problem statement

The interactive TUI shows enabled resources from the effective configuration and available resources discovered in configured Git registries. It does not show uninstalled resources exposed by configured Marketplace endpoints. Users therefore have to leave the TUI and use the Marketplace search command before they can discover those resources, even though the TUI already presents an Available section and can manage installed Marketplace pins.

A Marketplace may contain substantially more resources than a configured Git registry, and multiple Marketplaces may be configured at once. Loading every Marketplace resource before rendering would make startup dependent on network latency, consume unbounded memory and bandwidth, and make partial endpoint failures affect otherwise healthy local workflows. Marketplace search results are also discovery metadata rather than verified installation authority, so they cannot safely be handled as ordinary resolved resources.

## Solution

The TUI will render its authoritative local snapshot immediately and load a bounded first page of resources from every configured Marketplace in the background. Marketplace discovery will be independent of configuration resolution, materialization drift, and configured-registry catalog loading.

The existing `/` search interaction will remain an immediate in-memory filter for loaded rows. After a short debounce, the same query will also search configured Marketplaces asynchronously. Results from superseded queries will be cancelled and rejected by generation. Clearing the query will restore the cached empty-query browse page.

Marketplace discovery pages will use a bounded, per-endpoint disk cache. Fresh pages will render without a network request. Pages older than the fresh TTL but within the stale retention period will render immediately with a stale indication while they are revalidated. Successful endpoints will update independently; failed endpoints may retain stale cached results without affecting successful endpoints or authoritative TUI state.

Discovery rows will identify their Marketplace and source Registry and will preserve resource-coordinate and resource-revision identity. Blocked and revoked resources will remain visible for transparency but will not be installable. Installing an available Marketplace resource will resolve and verify its exact revision, present a preview derived from verified content for confirmation, write an exact pin to the selected scope, and then reload the authoritative snapshot.

## User stories

1. As a TUI user, I want available resources from configured Marketplaces to appear alongside available configured-registry resources, so that I can discover the full configured ecosystem without leaving the TUI.
2. As a TUI user, I want enabled resources to appear immediately when the TUI opens, so that Marketplace network latency does not delay my existing workflow.
3. As a TUI user, I want configured-registry resources to appear immediately when the TUI opens, so that remote Marketplace discovery does not block local catalog browsing.
4. As a TUI user, I want the TUI to load only a bounded first page from each Marketplace, so that opening the TUI does not download an entire large catalog.
5. As a TUI user, I want Marketplace loading progress to be visible without replacing the resource list, so that I understand why additional resources may still appear.
6. As a TUI user, I want Marketplace rows to appear as each endpoint becomes available, so that one slow Marketplace does not delay results from faster Marketplaces.
7. As a TUI user, I want the existing `/` interaction to search Marketplace resources too, so that I do not have to learn a second search mode.
8. As a TUI user, I want local filtering to react immediately on every keystroke, so that remote search latency does not make typing feel sluggish.
9. As a TUI user, I want remote search requests to wait briefly until I pause typing, so that ordinary typing does not issue a request for every intermediate query.
10. As a TUI user, I want results to correspond to my latest query, so that a slow response for an older query cannot replace newer results.
11. As a TUI user, I want clearing the query to restore the Marketplace browse page, so that I can move naturally between browsing and searching.
12. As a TUI user, I want search to narrow large Marketplace catalogs, so that the initial implementation does not need deep paging controls.
13. As a returning TUI user, I want recently fetched Marketplace rows to render from a local cache, so that reopening the TUI does not repeat unnecessary network work.
14. As a TUI user with a slow endpoint, I want recently stale results to render while they are refreshed, so that the list remains useful during revalidation.
15. As an offline TUI user, I want retained stale discovery results to remain visible, so that I can still inspect recently discovered resources.
16. As an offline TUI user, I want stale discovery rows to be labeled accurately, so that I do not mistake cached search metadata for current Marketplace state.
17. As a TUI user, I want corrupt or expired cache entries to be ignored safely, so that a bad cache record cannot break the TUI.
18. As a TUI user with multiple configured Marketplaces, I want a failure from one endpoint to preserve successful results from the others, so that partial outages have a narrow effect.
19. As a TUI user with multiple configured Marketplaces, I want stale fallback to be applied per endpoint, so that a failed endpoint does not cause successful endpoint results to become stale.
20. As a TUI user, I want Marketplace discovery failures to remain separate from snapshot errors, so that I can still manage healthy configured and enabled resources.
21. As a TUI user, I want each discovery row to identify its Marketplace and source Registry, so that I can distinguish resources with the same name.
22. As a TUI user, I want resources with identical names from different resource coordinates to remain separate, so that the TUI does not silently collapse distinct resources.
23. As a TUI user, I want an enabled Marketplace resource to take precedence over a matching discovery result, so that one logical resource is not shown as both enabled and available.
24. As a TUI user, I want resource kinds to remain part of identity and presentation, so that a skill and command with the same name are not confused.
25. As a TUI user, I want Marketplace descriptions and searchable metadata to participate in search where supported, so that I can find resources without knowing their exact names.
26. As a security-conscious user, I want all Marketplace-provided text sanitized at the render seam, so that remote metadata cannot emit terminal controls or misleading bidi content.
27. As a TUI user, I want blocked resources to remain visible but non-installable, so that Marketplace policy outcomes are transparent.
28. As a TUI user, I want revoked resources to remain visible but non-installable, so that revocation does not erase provenance information.
29. As a TUI user, I want cached discovery metadata to be treated only as a hint, so that stale search data can never directly authorize an installation.
30. As a TUI user, I want installation to verify the selected Marketplace instance, Registry, resource coordinate, revision, digest, assessment, provenance, and artifact, so that the written pin identifies verified immutable content.
31. As a TUI user, I want installation preparation to happen asynchronously, so that verification and acquisition do not freeze rendering or input.
32. As a TUI user, I want to confirm a verified installation preview before any installation is committed, so that exact identity, findings, and active capabilities are visible before configuration changes.
33. As a TUI user, I want installation previews to come from verified resource content rather than search metadata, so that an index response cannot misrepresent the artifact being installed.
34. As a TUI user, I want to select user, project, or local installation scope using the existing scope interaction, so that Marketplace resources follow the same configuration model as other resources.
35. As a TUI user, I want collection installation to verify and acquire every member before writing configuration, so that a collection is installed atomically.
36. As a TUI user, I want installation failure to leave configuration unchanged, so that network, policy, verification, or collision errors do not create partial pins.
37. As a TUI user, I want successful installation to trigger an authoritative snapshot reload, so that the resource moves from available discovery state to enabled state based on actual configuration.
38. As a TUI user, I want installed-name collisions to fail explicitly rather than silently renaming a resource, so that names remain predictable across installation sources.
39. As an organization administrator, I want Marketplace endpoint allowlist policy enforced before discovery and installation, so that the TUI cannot bypass effective configuration policy.
40. As an organization administrator, I want index-only acquisition origins validated against Registry policy during installation, so that trusting a Marketplace does not authorize an otherwise disallowed Git origin.
41. As a TUI user, I want remote discovery state to preserve my selected row when results are inserted or reordered, so that background work does not disrupt navigation.
42. As a TUI user viewing details, I want the open resource identity retained across accepted result updates, so that asynchronous refresh does not switch the resource I am inspecting.
43. As a TUI user, I want Marketplace result ordering to be deterministic for equal-ranked resources, so that refreshes do not cause arbitrary list movement.
44. As a TUI user, I want endpoint errors presented in deterministic order, so that diagnostics remain stable and understandable.
45. As a TUI user, I want the loading, stale, partial-failure, and truncated states to remain usable at the supported minimum terminal size, so that discovery does not regress core TUI accessibility.
46. As an operator, I want discovery operations logged with bounded counts, durations, cache outcomes, and failure outcomes, so that performance and reliability can be diagnosed.
47. As a privacy-conscious user, I want raw search queries excluded from logs and telemetry, so that discovery does not record potentially sensitive text.
48. As a maintainer, I want bounded cache retention and result counts, so that Marketplace discovery has predictable disk, memory, and network costs.
49. As a maintainer, I want Marketplace discovery policy centralized behind one interface, so that the TUI and other callers do not duplicate endpoint, cache, ordering, and failure behavior.
50. As a maintainer, I want realistic hot-path benchmarks retained in the repository, so that later changes can detect filtering, merging, sorting, or rendering regressions.

## Implementation decisions

- The authoritative TUI snapshot remains responsible for effective configuration, configured Git registry catalogs, resolved resources, enabled Marketplace pins, diagnostics, and materialization drift.
- Marketplace discovery is a separate module and state lifecycle. Its failures do not make authoritative rows stale, do not change drift assessment, and do not block mutations of healthy authoritative rows.
- The primary discovery interface accepts context, effective configuration, normalized query, resource-kind filter, and bounded result policy, and returns endpoint-granular outcomes. It hides endpoint resolution, allowlist validation, bounded concurrency, timeout handling, deterministic ordering, cache policy, and stale fallback from the TUI.
- Endpoint-granular outcomes retain canonical endpoint identity, configured Marketplace instance ID and alias, returned items, total count, truncation, cache state, observation time, and a typed endpoint error where applicable.
- Existing Marketplace client validation, redirect restrictions, response limits, URL normalization, instance verification, and error redaction remain authoritative and are reused rather than reimplemented in the TUI.
- The existing federated search behavior will be deepened or internally refactored so endpoint totals and identities are not discarded. The command-line search interface must retain its current external behavior.
- The empty query is a browse request. The TUI requests at most 50 results from each configured Marketplace and does not fetch subsequent pages automatically.
- Non-empty searches use the same per-endpoint limit. Users narrow result sets through search; manual and automatic deep-pagination controls are not included in the first implementation.
- Marketplace scores are treated as endpoint-provided ordering hints. The client does not normalize scores across independent Marketplaces. Equal-ranked results use deterministic Marketplace, Registry, kind, name, revision, and digest tie-breakers.
- The TUI continues to apply its local in-memory filter immediately. A Marketplace request is scheduled approximately 250 milliseconds after the last query edit.
- Starting a newer Marketplace request cancels the older request context and allocates a new process-local generation. A completion applies only when both its request kind and generation are active. An accepted completion cannot be replayed.
- Remote discovery completions can replace only Marketplace discovery rows and discovery diagnostics. They cannot replace configuration, resolved resources, drift state, snapshot diagnostics, operation diagnostics, or mutation state.
- An accepted discovery update restores list selection and open-detail identity using stable keys when rows are inserted, removed, or reordered.
- Authoritative rows and Marketplace discovery records remain distinct model concepts. Search records are not represented as verified resources and cannot enter materialization or drift calculation.
- A Marketplace discovery record carries its configured endpoint, Marketplace instance ID, Registry ID and slug, resource kind, registry-local name, revision, content digest, description, availability, metadata, score, cache state, and observation time.
- Resource-coordinate identity consists of Marketplace instance ID, Registry ID, resource kind, and registry-local resource name. Resource-revision identity additionally includes revision and canonical content digest.
- Exact row identity includes the resource revision so asynchronous updates are stable and distinct immutable revisions cannot be conflated.
- Display deduplication gives an authoritative enabled pin precedence over a discovery record with the same resource coordinate, even if the enabled pin references an older revision. This specification does not turn the discovery result into an update notification.
- Same-name resources from different Marketplaces, Registries, or resource kinds remain distinct rows.
- Marketplace discovery rows participate in the existing Available grouping. Registry-oriented grouping shows the Marketplace alias, or its normalized endpoint URL when no alias is configured, together with the Registry slug so remote and configured Git registry origins remain distinguishable.
- Marketplace-provided name, description, Registry slug, availability, and supported metadata may participate in the local searchable projection. Every remote field is sanitized at the terminal render seam.
- Search result metadata is explicitly labeled as discovery metadata in details. It is not used as a verified artifact preview.
- Blocked and revoked discovery records remain inspectable but cannot start an installation operation. Unsupported or unknown availability is represented truthfully rather than silently treated as installable.
- Marketplace progress is shown without activating the TUI's full-list loading state. Fresh, stale, refreshing, partially failed, and truncated discovery states are visible through bounded footer, section, or detail presentation.
- Discovery warnings have a lifecycle separate from snapshot and mutation diagnostics. Successful results survive partial endpoint failures, and endpoint failures are displayed in deterministic order.
- The search cache lives under the Marketplace cache root but in a separate namespace from immutable artifacts, collection manifests, and resource-revision metadata. Search cache contents never inherit verified-artifact semantics.
- Cache records are independent per canonical endpoint and query. A key includes cache schema version, canonical endpoint URL, configured Marketplace instance ID, normalized query, resource-kind filter, and requested page limit.
- Cache records are fresh for five minutes and eligible for stale-while-revalidate display for up to 24 hours. Older records are ignored and become eligible for bounded cleanup.
- The cache retains the empty-query page and up to 50 recently used non-empty queries. Retention is bounded deterministically rather than depending on map or filesystem iteration order.
- Fresh cache records render immediately and do not schedule a network request. Stale retained records render immediately with a stale indication and schedule asynchronous revalidation. Missing, expired, malformed, or unsafe records are ignored and fetched again.
- Cache reads and writes are isolated by endpoint. Corruption or write failure for one record does not invalidate other endpoint results or authoritative TUI state.
- Cache writes use safe containment checks, regular-file checks, bounded decoding, temporary files, and atomic rename. Search cache cleanup must not follow symlinks or remove unknown entries.
- When an endpoint refresh succeeds, its accepted result replaces that endpoint's cached or stale rows. When it fails, retained stale rows for that endpoint remain visible and are paired with a discovery warning. An endpoint without stale data contributes an error but no rows.
- Installation of a discovery row uses the existing serialized mutation-operation model. A second configuration-changing interaction remains blocked while preparation, confirmation, writing, or authoritative reload is active.
- Choosing an installation scope captures the exact selected discovery record before asynchronous work starts. Later discovery refreshes cannot change the occurrence being prepared.
- Installation constructs an exact Marketplace locator from the configured endpoint, Registry slug or ID, resource kind, registry-local name, revision, and content digest returned by discovery.
- The Marketplace resolver prepares the locator before any configuration mutation. Preparation verifies endpoint and instance identity, Registry identity and acquisition mode, exact resource revision and content digest, assessment and policy outcome, provenance, canonical artifact integrity, and applicable origin policy.
- Installation preview data comes from the verified prepared install. Findings, capabilities, license, provenance, acquisition type, exact digests, and collection members are never trusted from the search response.
- Every TUI discovery install requires confirmation of the verified preview. Existing risk-consent policy remains authoritative, and confirmation commits the same prepared plan used to produce that preview.
- Immediately before mutation, the pipeline reloads effective configuration and refuses the prepared plan if Marketplace or Registry allowlist policy, target harnesses, endpoint identity, or collision state changed after preview.
- User, project, and local scopes use the existing scope meanings and layered configuration paths. The prepared exact pin is written only to the selected scope.
- Collection preparation verifies and acquires the root and all transitive members before mutation. Configuration writes all exact pins or none.
- Installed names are never silently prefixed or renamed. A collision produces an actionable operation diagnostic; custom installed-name selection is outside this specification.
- Successful configuration mutation schedules an authoritative snapshot reload. Only the accepted healthy snapshot changes the row from discovery-available to enabled.
- Preparation, consent cancellation, or write failure leaves configuration bytes unchanged and preserves the discovery row. Errors remain operation diagnostics rather than discovery or snapshot failures.
- Configuration changes that alter effective Marketplace endpoints cancel and supersede discovery for the prior endpoint set. Results from no-longer-configured endpoints cannot be accepted into current state.
- Structured logging records operation boundaries, endpoint counts, success and failure counts, result and truncation counts, cache fresh/stale/miss outcomes, durations, cancellations, and generation rejection. Logs and telemetry exclude raw query text, response payloads, credentials, headers, artifact content, and unbounded identifiers.
- The TUI product contract will be extended to define Marketplace discovery as non-authoritative replaceable work, document stale-while-revalidate presentation and partial-failure behavior, and state that discovery metadata never authorizes installation.

## Testing decisions

- Tests assert observable contracts rather than restating helper algorithms, internal fields, cache-key construction code, or production sorting tables.
- The primary seam is the public TUI model and command/message lifecycle using isolated effective configuration, temporary cache roots, deterministic clocks, and local HTTP Marketplace endpoints. This is the highest seam that can observe immediate rendering, asynchronous delivery, navigation stability, partial failures, and installation handoff together.
- Existing Bubble Tea model tests provide prior art for sending messages and keys, observing rendered output, preserving selection, rejecting stale generations, and serializing mutations.
- Existing local Marketplace HTTP fixtures provide prior art for well-known identity verification, search envelopes, exact revision resolution, artifact acquisition, policy rejection, revocation, and collection preparation without external network access.
- Existing PTY E2E coverage for Marketplace-enabled TUI rows provides prior art for one focused end-to-end scenario proving that a remotely discovered resource can be searched, prepared, confirmed, pinned, reloaded, and rendered as enabled.
- Direct tests at the Marketplace discovery interface are added only for behavior that is impractical or ambiguous through rendered TUI output: fresh/stale/expired classification, endpoint-granular stale fallback, bounded retention, malformed-record isolation, atomic replacement, unsafe filesystem entries, and deterministic eviction.
- Initial-load tests prove that configured and enabled rows render before Marketplace completion and that the full-list loading state is not re-entered for discovery.
- Search tests prove immediate local filtering, debounce scheduling, query normalization, cancellation, generation rejection, duplicate-completion rejection, and restoration of the empty-query page.
- Race tests deliberately complete older requests after newer requests and assert that rows, query, cursor, open detail, diagnostics, configuration state, and drift state remain unchanged.
- Endpoint tests cover all success, all failure, partial failure, slow endpoint, cancellation while waiting for concurrency, timeout, invalid instance ID, allowlist rejection, malformed response, and deterministic error ordering.
- Cache tests cover fresh hits without network requests, stale display plus revalidation, stale retention after refresh failure, replacement after success, hard expiry, malformed JSON, unsupported schema, oversized records, wrong endpoint identity, interrupted writes, and independent endpoint corruption.
- Bound tests prove that no request exceeds 50 results per endpoint, no automatic next-page request occurs, the empty query and no more than 50 recent non-empty queries are retained, and result/cache growth remains bounded.
- Identity tests cover equivalent endpoint URL forms, aliases, distinct Marketplace instances, distinct Registry IDs, same-name cross-kind resources, exact revisions, installed-name aliases, and enabled-pin precedence over discovery records.
- Availability tests prove that allowed records can start preparation, blocked and revoked records remain visible, and non-installable records cannot mutate configuration.
- Installation tests prove that exact locator fields are retained from selection through preparation, verified previews are not sourced from search metadata, required consent is enforced, cancellation is byte-neutral, and preparation failure performs no configuration write.
- Collection tests prove that every member is prepared before mutation and that one member failure leaves all configuration bytes unchanged.
- Collision tests include a positive conflicting resource and prove that no silent rename or partial pin is produced.
- Policy tests prove that effective Marketplace allowlists and index-only Registry origin policy are enforced through the TUI path rather than only by command-line search or install commands.
- Partial-failure tests prove that discovery diagnostics do not set snapshot rows stale, do not report drift failure, and do not prevent mutation of healthy authoritative rows.
- Sanitization tests inject terminal controls, invalid UTF-8, Unicode separators, bidi controls, tabs, carriage returns, and structural newlines through every displayed Marketplace field and assert visible escaped output.
- Terminal-size tests cover loading, stale, partial-failure, blocked, revoked, and installation-preparation states at the supported minimum size, with required navigation, cancel, confirmation, and quit keys still visible.
- Determinism tests compare complete externally relevant ordering across repeated endpoint completion orders and filesystem iteration orders.
- Filesystem failure tests assert unchanged prior bytes and absence of temporary or staging residue.
- Logs and telemetry tests assert operation and count fields while proving that raw queries, credentials, headers, response bodies, and artifact content are absent.
- Benchmarks are added before changing user-visible hot paths. They cover merging and deduplicating realistic multi-Marketplace result sets, local filtering with authoritative and discovery rows, deterministic sorting, tree rebuilding, and rendering at representative terminal sizes.
- Benchmark baselines and post-change measurements use identical selections, repeated runs, and allocation reporting. Network and cache I/O inclusion is stated explicitly for each benchmark.
- Verification includes focused package tests, race tests, the relevant PTY E2E flow, project lint and build checks, and the repository's TUI QA contract checks.

## Out of scope

- Downloading or retaining the complete catalog of every configured Marketplace.
- Manual load-more controls, automatic infinite scrolling, or deep federated pagination.
- Changing the Marketplace server search algorithm, datastore indexing strategy, or ranking model.
- Normalizing relevance scores across independent Marketplace deployments.
- Popularity, recommendation, semantic-ranking, or usage-based discovery.
- A separate Marketplace tab or a second remote-search mode.
- Searching ad-hoc Marketplace URLs that are not present in effective configuration.
- Treating discovery results as resolved resources, materialization inputs, drift inputs, or verified previews.
- Automatically advancing an enabled pin when discovery observes a newer revision.
- Presenting update-available state based solely on search results.
- Custom installed-name entry or alias selection in the TUI.
- Installing blocked, revoked, rejected, or otherwise non-installable resource revisions.
- Offline installation when exact verified installation data is not already available through the existing resolver and cache contracts.
- Changing layered configuration precedence, Marketplace allowlist semantics, Registry origin policy, materialization behavior, or harness targeting.
- Changing command-line search output or documented command-line failure semantics.
- Live execution or agent-generated previews of discovered resources.

## Further notes

- This work completes the TUI discovery slice that was explicitly excluded from the original Marketplace serve and discovery design.
- The feature belongs under the existing Registry Discovery epic because it connects configured Marketplace search to interactive browsing and installation.
- The agreed initial constants are a 50-result per-Marketplace page, approximately 250 milliseconds of search debounce, a five-minute fresh TTL, 24 hours of stale retention, and retention of the empty-query page plus 50 recent non-empty queries.
- Marketplace search returns the latest listed result per resource coordinate, while immutable revision identity remains necessary for safe preparation and cache handling.
- A future pagination design should begin by defining endpoint-granular continuation and global merge semantics. Increasing a federated offset and requesting ever-larger prefixes from every endpoint is not an acceptable deep-pagination strategy.
- A future update-available design should use authoritative Marketplace revision inspection rather than infer update state from cached discovery results.

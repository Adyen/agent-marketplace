# Design

## Context

The resource assessment, artifact, and inspection projection already have separate identities. The built-in report currently lives in the assessment's `analyzer_version` and `findings` fields, and its policy decision controls availability. Ingestion is shared by the Marketplace workers and the index-only consumer's exact-commit verification, so sharing a package cannot imply that the scanner runs everywhere.

## Goals / Non-Goals

- Make multiple independently attributed reports visible without changing existing install decisions, findings, immutable content, or source-range behavior.
- Run both tools in the supplied deployments without increasing the trusted privileges of the Go worker or introducing a Python dependency for consumers.
- No analyzer plugin discovery, registry-authored configuration, LLM analysis, external dependency acquisition, or automatic blocking based on advisory findings.
- No requirement to backfill historical resource revisions. Normal refresh, reconciliation, and operator reindex can add reports to the current revision.

## Decisions

1. **Extend the resource assessment, not the revision or projection.** Add an optional ordered `advisory_reports` list keyed by tool ID. Each report has a version when known, a status (`complete`, `complete_with_caveats`, `partial`, `failed`, or `not_run`), normalized findings, and a bounded reason code when useful. Surface the built-in report in the UI from the existing fields rather than storing a second copy. Persist reports alongside the assessment through the same publication transaction; omit the new field from canonical encoding when empty so old digests keep working. Readers of either assessment endpoint see the same evidence. Keep absent historical reports distinct from stored failures.
2. **Run supplementary tools only through worker wiring.** Use a narrow scanner interface over a detached bundle (authored `SKILL.md` and the validated support-file set with relative paths). Inject the configured scanners in the Marketplace indexing roles, not in the consumer or unconfigured callers. Run supplementary work after the required per-registry build is complete, before publishing the assessment. Use separate per-bundle and aggregate time/size limits, bounded concurrency, and preserve head/generation fences. Budget exhaustion marks remaining work `not_run`; no scans continue after publication or cancellation. This avoids a second public state stream or a new durable queue, at the cost of a bounded increase in indexing latency and explicit omissions for large registries.
3. **Pin and isolate SkillSpector.** The runner runs the checksum-verified v2.13.6 distribution in a separate, non-root image with a read-only root filesystem and bounded temporary space. Accept only a typed, bounded contract for one bundle; reject links, paths outside the bundle, and malformed inputs. Run a fixed static CLI invocation; never accept provider settings, remote targets, custom rules, or suppressions from submitters. Use the same request protocol over a permission-scoped Unix socket on Compose (runner `network_mode: none`) and a private service on Helm (separate pod with mandatory ingress from the Marketplace indexer only and egress denied). Require an enforcing Kubernetes network-policy implementation; document how to verify isolation. Never mount registry checkouts, storage credentials, or a runtime socket into the runner.
4. **Treat scanner output as evidence, not authority.** Interpret exits 0 and 1 only with a valid, bounded structured report. Preserve the upstream coverage distinction, including offline advisory limitations. Use bounded rule IDs, severities, locally controlled summaries, and deterministic sorting; never persist raw report text, snippets, absolute paths, or risk recommendations. For the first increment, map exact matching support files to projection paths; omit primary-document and virtual-member locations unless verified against the projection, since its skill body excludes frontmatter. Timeouts and malformed results emit bounded status/reason codes only.
5. **Keep one Security tab.** Show the Marketplace policy line and the existing assessment identifiers once. Stack tool sections in a stable order on desktop and mobile, with each tool's coverage indicator paired with its own findings, not a joint badge. Retain the current built-in range navigation. Absent SkillSpector data on a skill displays "No SkillSpector report for this revision"; an explicit failed or not-run result gets its own status. Non-skills show only applicable reports.

## Risks / Trade-offs

- Scanner regressions and false positives: no effect on install decisions; preserve tool attribution and avoid `SAFE`/`DO_NOT_INSTALL` labels.
- Scan time and outages: enforce independent time and resource budgets; report omissions and preserve normal publication. Verify the maximum admitted batch under the existing refresh lease and cancellation limits.
- Egress and private skill contents: validate containment in a disposable runner; require default deny, run with no credentials, and prove a real scanner invocation cannot make an outbound request.
- Existing assessments and mixed deployments: verify byte-for-byte digest stability for old records. Deploy upgraded readers before enabling publishers that write advisory reports. During rollback disable report-writing first; keep report-aware server readers for records already stored.

## Migration Plan

Build and release the scanner image with immutable production references. Upgrade Marketplace API/web readers and then enable the report-writing worker with the default deployment configuration. Existing head content remains installable; a normal refresh or explicit reindex produces a new assessment without reinstalling or changing pins. Disable the runner for operational recovery without changing built-in analysis.

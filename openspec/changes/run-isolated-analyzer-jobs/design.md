# Design

## Context

`pkg/marketplace/ingest/ingest.go` already selects the authored `SKILL.md` and bounded support files for both acquisition modes. It currently scans them before returning the registry revision. A 34-second registry budget can expire before a skill is attempted. `deploy/skillspector/runner.py` translates a private HTTP request into a static CLI invocation and translates the native JSON back into a smaller report. Keep the input selection and publication policy, not that protocol.

## Goals / Non-Goals

Prove one trip from a third-party registry commit to a visible, revision-bound finding without installing Python in the worker. Then make the same path reliable across refreshes and configured analyzers. This is not a generic container scheduler, a public report upload endpoint, a vendor recommendation-based install gate, or a migration of Compose. No Kubernetes permissions move into the indexing worker.

## Decisions

### First milestone: prove the file handoff before building a queue

Use an index-only fixture registry with no CI and a pinned stock CLI image. A trusted harness uses the same bundle selection and original relative paths as ingestion. It stages one skill, not the registry checkout. Run this in a disposable namespace with enforced network isolation; rendering a Job or feeding a canned report to a parser is not proof.

| Step | Who can see what |
| --- | --- |
| Stage | A trusted writer places regular files on a fresh, attempt-scoped input volume. It rejects links, path collisions, out-of-root paths and budget overruns before launch. No public artifact or Secret/ConfigMap holds source bytes. |
| Scan | A single non-root container mounts input read-only, separate bounded output and temporary scratch writable. It has a read-only root, no credentials, token, host mounts, runtime socket or permitted network peers. No credentialed sidecar shares this pod. |
| Collect | After the scanner has stopped, a separate trusted reader mounts only that attempt's output read-only. It admits one bounded regular file at a fixed path, refuses links or special files, and normalizes findings against the staged path manifest. Never ingest raw stdout, stderr or report text into Marketplace logs or public findings. |
| Reclaim | Release mounts and delete both volumes and the Job on success, malformed output, timeout or cancellation. Reconcile orphans after restart. Use a disposable storage class with delete reclamation; deny the mode when isolation or reclamation cannot be provided. |

Use separate short-lived PVCs for the proof. A pod-local `emptyDir` cannot carry a file to a collector in another pod. Conversely, uploading the bundle to the public blob store or giving the scanner a storage URL would violate the index-only or offline boundary. Bound the number, size and lifetime of volumes as well as per-file and report sizes.

For SkillSpector, use fixed argv: `skillspector scan /input --no-llm --format json --output /output/report.json`. An ordinary CLI image built from the pinned NVIDIA release is sufficient; do not assume a published vendor OCI tag. `--no-llm` is not network isolation. Accept exits 0 and 1 only with a valid report indicating successful execution; 1 can mean findings, so a Kubernetes Failed Job alone cannot decide the analysis outcome. Use one pod per attempt and inspect its termination status before cleanup. Reject exit 2, timeout, malformed or mismatched output. Preserve native coverage (`partial` is not clean), known rule codes and verified locations; redact everything else. Verify the reported version against the selected operator profile rather than one global application constant.

### Record work, not source archives

For Job mode, add durable work records alongside publication metadata in SQLite and PostgreSQL. Enqueue in the transaction publishing an indexed skill, not after a successful refresh response. Key work by Marketplace/registry/resource coordinate, content and inspection identity, exact scan-input digest, and analyzer ID + image/configuration fingerprint. Record the exact commit/path as provenance. Reconcile current heads when enabling an analyzer or changing its configuration. Use an unambiguous canonical digest of sorted relative names, lengths and original bytes, including the primary document's frontmatter.

Only metadata survives the attempt. To retry, the trusted acquisition path reopens or reacquires the **recorded commit** and verifies the input digest; it does not fetch the current branch and silently substitute its contents. If the commit is unavailable or exceeds limits, record that outcome. Input is staged by a trusted component; the analyzer has neither repository credentials nor an API client. The dispatcher works in a dedicated namespace with narrowly scoped RBAC and admission controls for fixed Job and volume templates. A registry cannot supply a PodSpec, image, argv, shell fragment, URL or mount.

Lease work with bounded concurrency, deadlines, retry limits and backoff. Lease expiry permits recovery; a new attempt gets fresh storage and an attempt identity. Duplicate completion is idempotent; superseded attempts cannot update a newer result. Limit outstanding work per registry without dropping its backlog. Configure analyzers as an operator-owned list of stable IDs, immutable images, fixed invocations and known report adapters. Only the SkillSpector adapter ships initially; a second inert test analyzer proves independence without inventing a plugin system.

### Publish evidence without changing install policy

Keep the built-in assessment available while work is outstanding. Expose outstanding work as not-yet-scanned, distinct from failed and from a completed report with no findings. Accept completion only for the leased input and analyzer fingerprint. Merge reports in stable analyzer order, with a transaction and a compare-and-swap on the current revision, configuration and assessment pointer. Concurrent completions must not overwrite each other. Use `FinalizeResourceAssessment` to produce new immutable evidence; old assessment digests retain their original meaning. A removed analyzer or superseded revision cannot acquire a current pointer through late delivery.

## Risks / Trade-offs

- **Pods and volumes per attempt cost time.** Measure end-to-end latency, queue age and cleanup in the first witness. Do not add a pool or make registry publication wait on Job startup.
- **A Job controller can create workloads even without Secret read access.** Isolate its namespace and restrict service accounts, pod security, volume sources and images at admission. Do not grant generic pod creation or `pods/exec` to the worker.
- **Malformed output and storage leaks expose private content.** Enforce size limits before decoding, non-following reads, bounded diagnostics, storage quotas and cleanup on every exit. An isolation, attribution or cleanup failure stops the rollout.

## Migration Plan

Milestone one is an end-to-end witness only. Proceed to durable dispatch and the opt-in Helm mode only when that passes. Keep HTTP and Job modes mutually exclusive for the same analyzer, preserve the current deployment defaults, and do not change Compose or the consumer CLI. On rollback, stop admitting new Jobs, fence in-flight results and drain/expire owned resources before disabling the dispatcher. Previously issued assessment digests remain readable. Replacing the default runner or introducing a publication gate requires a separate decision.

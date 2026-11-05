## Boundary and decision

Alias admission and bounded target reading already use captured filesystem authority, but the original helper returned only bytes and permissions and closed that authority before pathname replacement/removal. A deterministic payload-free observation point reproduces native synchronization overwriting a concurrent regular destination for both nonempty and empty captured targets.

Keep capture, read and detachment in one shared operation. Capture the logical alias identity through a descriptor-bound native directory handle and retain the existing target destination throughout the operation. Revalidate named parent reachability, logical identity and original target state before staging and again before publication. Compare current logical alias resolution with the original captured target path so retargeted source-directory aliases cannot authorize stale bytes; this comparison never replaces the original identity or reads a new payload. Reuse descriptor-relative native rename for logical replacement and the existing generation-checked logical removal for empty input. Existing destination publication continues to write through supported aliases rather than acquiring new detachment semantics.

Replacement files begin private, are written with the original shared permissions and synced, and have their opened identity checked against the named stage before publication. Cleanup remains below the captured parent and removes only matching attempt-owned staging; a foreign staged replacement is preserved and cannot become publication authority. Meaningful close failures remain errors.

## Compatibility and limits

Preserve contained aliases, trusted-cache migration, authorized initial scope/root aliases and machine relocation behavior. Keep actual native marker merging, original target bytes/identity/mode/mtime, existing shared permissions, empty-target private creation and subsequent byte/identity/mtime no-ops. Read-only operations revalidate captured alias state without detaching it; unrelated read-only planning behavior is not redesigned.

Observed drift returns the existing generation-conflict identity and native materialize.write.error diagnostic. This is generation-checked action, not an atomic conditional rename/unlink against hostile same-user mutation after the last check, automatic retry, a new ownership model or a whole-operation transaction.

## Evidence and cost

Exercise actual Factory, Cursor and Windsurf native repository/machine synchronization with empty/nonempty targets, mutating/read-only input boundaries, post-staging replacements, same-text aliases, identical-byte target replacements, in-place target edits, disappearing aliases and replaced/aliased parents. Assert original, concurrent and retained file bytes/identity/mode/mtime, narrow refusal, owned-stage cleanup and actual healthy retry. Separate staging replacement controls prove foreign staging is neither published nor deleted; empty controls prove private native creation and repeat no-ops. Existing contained-alias controls preserve nonempty shared-file permissions and handwritten entries.

Measure the existing BenchmarkMarkerAliasMCPSyncDisk before and after, with its native filesystem I/O and alias recreation in the timed workload and no network or executed resource payloads. Verify affected packages, shared consumers, CLI conversion/deinit, Linux/macOS, repeated races, strict specs, combined E2E/configuration compatibility and adversarial review.

## Root cause and decision

Merge preparation reads existing JSON before artifact comparison captures the identity later used for publication. The two steps can therefore observe different generations: output is merged from A, but publication is authorized against B.

Reuse the existing descriptor-based file comparison reader as a shared payload-plus-identity reader for merge input. Keep original marker-merge identities in the existing publication identity map, including a present map entry with nil identity for genuine absence. Comparison must match that original input state or absence before it may plan a write or no-op. Existing final publication checks continue using the same admitted state.

The reader must reject non-regular, aliased and multiply linked payloads without blocking, retain captured-size streaming checks, validate opened and named generation, and propagate close failures. No absolute size ceiling is added. A payload-free debug event identifies the merge-input operation boundary; existing test observers use that event to schedule deterministic inert mutations through public consumers.

## Compatibility and alternatives

Keep public APIs, marker merge semantics, staged-all-artifact behavior, dry-run and partial-result contracts unchanged. Native aliases that are already explicitly admitted and detached before SDK publication retain their existing controls. Other output/root/namespace and durable ownership consumers are not redesigned.

A new root lock would only serialize cooperating SDK callers and would not bind an external editor's generation. Automatic retry/remerge would change failure and authorization behavior. Neither is needed to preserve observed concurrent generations: reuse the existing generation-refusal contract instead.

## Evidence and cost

A real Factory native MCP installation supplies original aggregate A. At merge-input capture the regression retains A and writes independently authored concurrent B. The unfixed public sync overwrites B with stale merged bytes and succeeds. The fix must preserve B and A bytes/metadata, emit the existing coded refusal, leave no attempt stage, and a healthy retry must actually publish desired content while keeping B's handwritten content.

Extend with public SDK original absence, in-place edits, same-byte replacements, read-only and valid controls. Verify affected native targets and direct conversion consumers, Linux/macOS, races, strict specifications, scoped validation and combined E2E/configuration compatibility. Measure public SDK merge updates with disk I/O timed, fixture reset excluded and no network requests.

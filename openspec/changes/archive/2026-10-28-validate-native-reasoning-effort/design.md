## Approach

Keep validation in the shared model resolver so every compiler follows the
same policy. The same-harness branch must preserve model spellings but must
not treat every separate reasoning field as representable.

Reuse the catalog's existing native encoding index. Retain the initial
exact native model/effort match as a fast path: native encoding values need
not be identical to canonical reasoning names. For a known identity that is
available in the destination catalog, validate unmatched preferences against
its native model encoding. A resolved family identity uses its pinned record's
capabilities without rewriting the authored model string.

An identity known only to another harness target is still unknown for this
destination. Preserve its native model and reasoning, just as for a future
unregistered model. Inheritance and model-encoded reasoning retain their
existing branches. Existing loss-to-diagnostic translation supplies the
warning; serializers already omit empty effort values.

Retain the legacy Codex omission code for Codex-sourced selections. Its
catalog description must also cover same-target unsupported reasoning rather
than incorrectly promising that compiling to Codex always preserves effort.

## Verification

Use public `Convert` with native Factory source files to prove the complete
ingestion/provenance/resolution/serialization path. Assert model and body
preservation, true reasoning-key absence, and the exact warning. Add supported
and unknown-model controls, a pinned selector, and a data-level native encoding
whose value differs from its canonical reasoning name.

Capture before/after resolver timing and allocations with five two-second
samples. Run compilation-library validation and spec checks, archive the
change, and perform adversarial OCR delegate review before publication.

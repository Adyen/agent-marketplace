## Why

Hook timeouts had no specified lower or upper bound (#652). A zero timeout passed through on some targets and was refused on others without a stated rule. Codex's documented one-to-three-second `SessionEnd` bound capped values above three but let `0` through. Large values silently changed: a timeout of `9007199254740993` seconds was emitted as `9007199254740992`, and converting seconds to Gemini's milliseconds could overflow and wrap. The integer-metadata requirement forbids both. The behavior of a hook source that spells a modeled field twice in different letter case was also unspecified at the consumer.

## What Changes

- A zero timeout passes through unchanged wherever the target emits timeouts and declares no minimum.
- A timeout below a documented per-event minimum is raised to the minimum, with the same warning as the existing cap. Per-event bounds apply before the exact-range check, so a bounded event never refuses a value it can clamp.
- A timeout whose value in the target's unit exceeds 2^53-1, the largest integer every JSON reader decodes exactly, is refused with the target's timeout-unsupported error, including after a seconds-to-milliseconds conversion. Smaller values are emitted exactly.
- A registry whose hook source declares an ambiguous typed field fails to load with `json.duplicate-key`. `sync` and `sync --check` refuse without changing any installed output and without quoting the payload.

## Capabilities

### Modified Capabilities

- `hook-resources`: hook timeouts stay exact within documented bounds, and ambiguous hook sources fail closed.

## Impact

`hookpolicy` timeout resolution: a new exact-range refusal and a per-event minimum. Hooks with timeouts above 2^53-1 seconds (or above 9007199254740 seconds on Gemini) are now refused instead of emitting a changed value. A Codex `SessionEnd` or `Interrupt` timeout of `0` becomes `1` with `compile.codex.field-dropped`. No other emitted bytes change.

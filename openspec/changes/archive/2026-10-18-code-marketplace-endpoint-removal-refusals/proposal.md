## Why

Marketplace endpoint removal refuses unsupported qualifiers, missing
selections, and URLs that also identify a Git or directory source, but those
refusals have no stable diagnostic code. Callers and tests can only match prose.

## What Changes

- Prefix each refusal with a stable code, following the existing
  `marketplace.install-collision` form: `marketplace.remove-unsupported-qualifier`,
  `marketplace.remove-not-selected`, and `marketplace.remove-ambiguous-source`.
- Expose each code as a sentinel error so callers can use `errors.Is`.
- Refusal conditions, message text after the code, and the absence of writes
  are unchanged.

## Capabilities

### Modified Capabilities

- `resource-removal`: name the diagnostic codes of these three endpoint removal refusals.

## Impact

Shared removal pipeline and its regression test. No configuration, lock, or
flag changes.

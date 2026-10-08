## Context

Git builds a relative include path by appending the value to the declaring file's path up to its last slash, and a `~/` path by appending the value to HOME. It never cleans the result, so the kernel resolves `..` through symlinked directories. Git expands `%(prefix)/` against its runtime prefix, which only the installed Git knows.

## Decisions

- Concatenate relative and `~/` include paths exactly as Git does instead of cleaning them.
- Ask the installed Git to expand `%(prefix)/` values with `git config --type=path`, passing the value as environment configuration with every configuration file disabled. The value needs no quoting and is never treated as an include. Output is bounded and stderr is discarded.
- Keep every expanded path subject to the existing trust checks, cycle detection, and budgets.

## Validation

A differential test uses native Git as the oracle for relative and `~/` includes whose `..` follows a symlinked directory and for a `%(prefix)/` include.

### Executed evidence

- The differential test failed before the fix, reading the lexical decoys and dropping the prefixed include, and passes after it. Git 2.54 resolves the same files.
- Focused authentication tests, race checks, scoped `make validate` (cache package tests, CI lint, build), and `make spec-check` pass.
- An independent adversarial review found no critical or high issue. Its medium finding, this missing spec delta, is addressed here. Its low findings were declined: Git 2.54 treats `:(optional)` include values literally, which the reader matches. Cancellation during prefix expansion reports the same untrusted-configuration error as the existing parser subprocess. Acquisition already requires Git 2.31 for environment configuration.

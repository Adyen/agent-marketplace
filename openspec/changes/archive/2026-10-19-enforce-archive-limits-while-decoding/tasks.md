## Implementation

- [x] Add failing regressions for each limit, followed by a tripwire entry that decoding would otherwise reject, plus a within-limits control.
- [x] Enforce the archive limits while decoding in the digest, placeholder and content verification functions, leaving the unchecked decoder unchanged.
- [x] Prove each limit with a negative mutation and run the Marketplace client, server and library consumers.
- [x] Verify scoped tests, lint, build and strict specs.
- [x] Adversarially review every changed entry.
- [x] `openspec archive enforce-archive-limits-while-decoding --yes`

## 1. Regression coverage

- [x] 1.1 Add failing command tests with a fake Marketplace: repository config selecting one removed and one available resource with no lock (sync warns, writes a lock with only the available record, installs it, exits 0); the same with an existing lock missing the removed record; a required removed selection (fails, no lock written); a 5xx on the same lookup (fails, no lock written).
- [x] 1.2 Add failing command tests proving `remove <name> --scope repo` succeeds with no lock and with a lock lacking the record, edits only configuration, and the following `sync` exits 0.

## 2. Implementation

- [x] 2.1 Let named Marketplace removal proceed without an acquisition lock record for the selection.
- [x] 2.2 Classify a latest-revision 404 during lock record acquisition for a non-required selection as a warning and continue with the remaining selections; keep required selections and other failures fatal.

## 3. Verification

- [x] 3.1 Run focused pipeline, marketplace and command tests, `make spec-check`, and `make validate`.

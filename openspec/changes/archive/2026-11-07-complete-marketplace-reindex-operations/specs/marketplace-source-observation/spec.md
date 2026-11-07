## ADDED Requirements

### Requirement: Separate worker processes sharing one store do not multiply upstream work

Two or more `serve` worker processes that share one metadata store SHALL together observe each due registry once per due slot and SHALL run one refresh per changed commit, in SQLite and in PostgreSQL.

#### Scenario: Two worker processes probe and refresh a changed registry once

- **WHEN** two `serve --role worker` processes share one metadata store and observe one configured local Git registry that receives one new commit
- **THEN** the processes' observation counts summed rise by at most one per due slot, their `source-change` refresh counts summed rise by exactly one, and the registry history gains exactly one occurrence for the new commit

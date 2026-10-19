# mutable-ref-cache Specification

## Purpose
Define how mutable acquisition keeps the working checkouts of Git registry refs separate, so Git never acts on another ref's checkout or on a repository enclosing the cache.

## Requirements

### Requirement: Each ref's mutable cache directory is its own checkout

Mutable acquisition SHALL clone, fetch, or check out a Git registry ref only in a mutable ref cache directory that is that ref's own checkout. It MUST refuse, before running Git or creating directories, a ref whose cache directory lies inside another ref's cached checkout, and a ref whose existing cache directory is not its own checkout. The refusal MUST name the ref and its cache directory, and MUST leave every other checkout's HEAD, refs and files unchanged, including any repository enclosing the cache. Refs whose cache directories only share intermediate directories MUST remain independently usable.

#### Scenario: Nested ref inside a cached checkout is refused

- **WHEN** a ref is cached and a ref nested below it from the same registry is acquired, whether or not the nested path exists in the cached ref's tree
- **THEN** acquisition of the nested ref MUST fail, naming the nested ref and the enclosing checkout
- **AND** the enclosing checkout's HEAD, refs and files MUST be unchanged, no directory MUST be created inside it, and the enclosing ref MUST still refresh

#### Scenario: Ref cache directory without its own repository is refused

- **WHEN** a nested ref is cached, a Git repository encloses the cache directory, and the ref above the nested ref is acquired
- **THEN** acquisition MUST fail, naming the ref and its cache directory
- **AND** the enclosing repository and the nested ref's checkout MUST keep their HEAD, refs and files

#### Scenario: Sibling slash refs remain independent

- **WHEN** refs sharing only an intermediate directory, such as `feature/a` and `feature/b`, are acquired and refreshed from the same registry
- **THEN** each MUST succeed in its own cache directory as the root of its own checkout, with its own content

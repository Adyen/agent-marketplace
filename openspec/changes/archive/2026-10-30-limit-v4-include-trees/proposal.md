## Why

Config include cycle detection only tracks the current include chain, so a fragment shared by two branches is read and parsed once per branch. A cloned repository can commit a small include DAG that expands exponentially and stalls every command that loads repository config: 43 files at depth 14 cause 65,533 reads and take 2.6 s, and each further level doubles the work (#434, R51).

## What Changes

The maintainer-approved D2 decision for #434 limits one root load to 256 config files, counting the root and every include occurrence on every branch. A larger tree refuses with `v4.include-limit` before any acquisition, lock or harness work. Include precedence, ordering, multiplicity and branch-local cycle detection are unchanged for trees within the limit.

## Release note

A config file whose includes expand to more than 256 config files is now refused with `v4.include-limit` instead of being loaded. This prevents a repository from stalling commands with a deeply nested shared include tree.

## Capabilities

### Modified Capabilities

- `consumer-config-format`: include-tree limit.

## Impact

Affects `internal/config` v4 tree loading, which every root load shares. This is a new D2 limit approved in #434; it has no override and adds no dependency.

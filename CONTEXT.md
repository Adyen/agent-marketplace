# Agent Marketplace

Agent Marketplace distributes harness-neutral agent resources and materializes them into layouts understood by agent runtimes.

## Language

**Harness target**:
A destination runtime format into which harness-neutral resources are compiled.
_Avoid_: Provider, compatibility layout

**Source layout**:
The canonical on-disk format from which a resource bundle is ingested.
_Avoid_: Harness target, import path

**Converted command skill**:
A command represented as a skill in a harness that has no command primitive while retaining its command identity.
_Avoid_: Native command

**Owned entry**:
An element inside a shared harness configuration file that Agent Marketplace is demonstrably authorized to reconcile and prune.
_Avoid_: Generated file, managed file

**Ownership state**:
Evidence identifying owned entries in shared harness configuration files.
_Avoid_: Cache, ownership marker

# ADR 0008: Use layer-owned Git acquisition lockfiles

**Status:** Accepted

## Decision

Agent Marketplace records exact Git acquisition results in one generated lockfile owned by each user, repository, or personal-local configuration layer, and optionally by the organization layer. The corresponding files are `~/.agents/lock.yaml`, `.agents/lock.yaml`, and `.agents/lock.local.yaml`; an organization may deploy `/etc/agent-marketplace/lock.yaml` beside its config. The repository lock is committed and the personal-local lock is ignored.

Each lock records canonical Git origin and declared ref, the exact algorithm-qualified commit, every configured source root, the detected source harness, and the source revision manifest. Consumer commands compose exact layer catalogs before applying normal configuration precedence and selection. Plain filesystem directory registries remain live and unlocked. A hostless `file://` URL ending in `.git` is a supported lockable Git origin.

Normal sync minimally reconciles writable locks when configuration changes, while `lock --upgrade` and `update` explicitly advance compatible Git resolutions. Read-only commands never reconcile locks. A missing organization lock is permitted and remains unlocked; when one is present it is validated as part of the organization trust boundary and consumers never replace it implicitly.

Configuration and its lock are committed through one dependency transaction so a failure cannot publish only half of the dependency state. Lock output is deterministic, strictly decoded, path-safe, and written atomically without rewriting unchanged bytes. Exact Git snapshots are opened by commit and verified against their manifests; mutable ref caches are not accepted as substitutes.

## Alternatives considered

A single effective-environment lock was rejected because it would mix organization, user, repository, and personal-local state into a machine-specific snapshot. Inline commit resolutions were rejected because generated resolution churn would remain mixed into hand-authored policy. Locking plain local directories was rejected because it would break their deliberate live development workflow.

## Consequences

Layer precedence remains configuration-owned, while acquisition becomes reproducible per layer. Repository consumers can reproduce committed Git inputs, user and local intent remain private to their owners, exact snapshots can be garbage-collected from the active lock set, and stale or malformed dependency state fails closed before mutation.

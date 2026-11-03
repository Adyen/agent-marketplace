# ADR 0011: Cooldown clocks and history-rewrite refusal

**Status:** Accepted (2026-11-02)

## Decision

A cooldown delays candidate selection until a resource revision or Git commit has been observable for a configured period, so that a compromised registry's malicious change can be noticed and revoked before consumers advance to it. The cooldown gates only advancing: an existing pin or lock is never re-evaluated, and `sync` in any mode never consults ages.

Each source kind uses the only clock that is trustworthy for it:

- **Marketplace sources** measure from **first publication**: the time the Marketplace first recorded a registry occurrence containing the resource revision. It is server-assigned, keyed by content digest, and unchanged by reassessment, archive-digest repair, or registry moves. A revert to previously published content is therefore immediately eligible, and a newly approved registry or a from-scratch metadata database holds everything for one period.
- **Git sources** measure from the **effective commit date**: the later of a commit's committer date and the effective commit date of its first parent (and, for annotated tags, the tagger date). A commit is never considered older than its ancestors, so a backdated commit pushed on top of a recent tip is simply young. The walk is anchored at the locked commit; the candidate is the newest first-parent commit old enough that is also a descendant of the locked commit, which may be older than the tip.
- A consumed branch whose locked commit is no longer an ancestor of its tip is a **history rewrite** and is **refused**, not aged out. Acceptance is an explicit, optionally targeted flag on `update` and `lock --upgrade` (and a confirm modal in the TUI); the resulting lock is the only record of acceptance. `--bump` across tags requires no ancestry and measures back to the merge-base.
- **Initial acquisition** is deliberately asymmetric. A Git source with no lock has no anchor, so if its whole history is young the tip is taken with a warning: the person writing `git: <url>` made the trust decision. A Marketplace coordinate with no pin whose revisions are all young requires an explicit acknowledgement, because catalog names are confusable and typosquats live there. Operators and organizations can make either strict.
- The cooldown is a **floor that layers may only raise**: Marketplace operator (server-side), then organization, user, repository, personal-local. A lower value is clamped to the floor with a warning. The built-in default is zero; documentation recommends a 24-hour organization floor.

Normative behavior belongs to the OpenSpec specifications that implement this decision; this ADR records why the clocks and refusals are shaped as they are.

## Alternatives rejected

- **Commit or assessment time as the Marketplace clock.** Commit dates are written by whoever controls the repository; assessment time resets on every analyzer or policy re-run.
- **Local first-observed time for Git**, alone or combined with committer dates. It resists backdating, but every update becomes a two-step wait and it gives no answer for initial acquisition, which is exactly when a new consumer is most exposed. The causality rule covers the frequent-push case without that cost.
- **No Git cooldown.** Rejected because the effective-commit-date rule with a locked anchor does hold against a compromiser who pushes onto a live branch; only history rewriting defeats it, and that is refused separately.
- **Aging out a history rewrite** instead of refusing it. Waiting would launder the one event that defeats the Git clock.
- **Hiding held revisions server-side by default.** It would prevent clients from offering initial-acquisition acknowledgement; hiding remains available as the operator's `held: block` mode.
- **A runtime flag to bypass a cooldown hold.** A hold is a policy outcome; the reviewable bypass is an exact pin in committed configuration (full commit ID or exact `revision:`), which an organization layer can see and constrain.
- **Database-managed cooldown in the first change.** Cooldown is a distribution policy like `rejected_resources` and will follow it into the managed snapshot; the first change keeps it in operator YAML with file-mode per-registry overrides.

## Consequences

- After an accepted history rewrite the Git cooldown offers no protection against an attacker who also wrote the dates; the refusal and its evidence are the only control, and the refusal text must say so.
- First acquisition of a Git source trusts the publisher's dates by design. A new consumer adding an established repository still gets the newest old-enough commit, not the tip.
- Adding a new resource to an already-locked Git source is an update of that source and is held like any other; commit-level locking cannot distinguish it from a malicious addition.
- Raising a floor never disturbs existing installs; it only delays future advances. Candidates never move backwards.
- Clients compute the effective Marketplace cooldown as the maximum of the server floor and their own; against a server that does not expose first publication, the client warns and proceeds unless configured to refuse.

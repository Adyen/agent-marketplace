## Scope and root cause

Portable manifest admission checks only lexical containment below the scope base, plus the repository Windsurf namespace. Pruning then resolves a nomination below the harness target root and deletes it when the recorded digest matches the current bytes. Every harness target root also holds files that file-mode materialization never records: shared harness configuration files, vendor settings and, in the Codex `.agents` partition, Agent Marketplace configuration and lockfiles. A component directory nominated as a whole is hashed and removed as one tree. A digest is not proof of authorship, because committed files have publicly known bytes and modes.

## Decision

Admit a nomination only when it names one resource inside a component directory. Relative to the harness target root (below `.windsurf/` for repository Windsurf, below the partition prefix for Codex), the cleaned path must contain a component element followed by at least one further element. Every writer records exactly that shape: resource trees and files are published as `<component>/<resource>` and support files as `<component>/<file>`, while shared harness configuration files are merge artifacts reconciled through markers or ownership state and never enter the manifest.

Some shared aggregate files live inside a component directory: `hooks/hooks.json` for Claude, Factory and Cursor, and `.claude-plugin/plugin.json` for Claude. The structural rule alone admits them, so a nomination that equals, or is an ancestor of, an aggregate file the harness target declares for the scope is refused as well. The aggregate list comes from the same target metadata that drives merge reconciliation; for Codex it comes from each partition's native layout. The comparison is case-insensitive per path element, because the default macOS and Windows filesystems resolve a variant such as `HOOKS/Hooks.json` to the aggregate itself. On a case-sensitive filesystem this refuses a differently cased path that no writer produces.

Codex nominations must also use a component that their partition declares in the native layout, because `.agents` is shared with Agent Marketplace configuration, includes and lockfiles. The native layout is the topology authority that Codex planning already uses, so the admission rule cannot drift from installation.

Single-root harness targets keep the structural rule without a component allowlist. Their declared component lists are not exhaustive: Gemini publishes subagents below `agents/` although its declared components are `skills` and `commands`, and rules publish below `rules/` for Factory, Cursor and Windsurf. An allowlist would refuse valid manifests and block every target's materialization for that scope.

The check runs at the shared manifest-loading boundary in both scopes, before nominations can authorize ownership hashing, replacement, removal or manifest publication. It returns the existing unsafe-path identity, so materialization and scoped command consumers emit `materialize.manifest.error` and leave the manifest unchanged. Entries for unknown harness targets are never consumed and remain outside the rule. The repository Windsurf namespace check from the previous change becomes one case of this rule, with its message preserved.

## Compatibility and non-goals

File-mode lifecycle inventories at v3.1.1, v4.0.0 and the current head, covering skills, commands, subagents, hooks, MCP servers and rules for every harness target in both scopes, recorded only `<component>/<resource>` nominations (and Codex partition-prefixed forms of the same shape), none equal to a declared aggregate file. Legacy Windsurf `hooks/` support entries stay admissible and keep their preservation policy. Existing manifests therefore remain admissible.

One narrow historical shape is refused deliberately. From v4.0.0 until v4.0.12, hook support collection excluded the primary definition and its aggregate siblings but not a literal `hooks.json`. A registry that placed a `hooks.json` next to a differently named hook definition could therefore record `hooks/hooks.json` for Claude, Factory or Cursor. That entry claims the harness target's own hooks aggregate, the shape this change exists to refuse, so the scope reports `materialize.manifest.error` until the entry is removed from the manifest.

This is namespace containment, not cryptographic provenance: a content-identical nomination of any other resource inside a component directory keeps its existing ownership contract. SDK synchronization still owns its caller-authorized explicit output directory. The accepted narrow quarantine deletion boundary is unchanged.

## Evidence and verification

Regressions independently author manifests and digests; they cross-check the authored digest against the current bytes only to prove that the fence, not a digest mismatch, causes refusal. Positive controls install and genuinely prune native output for each harness target and scope. Unfixed consumers delete the nominated root files, in-component aggregate files, component directories and Codex shared-partition files with no diagnostic. Fixed consumers must preserve bytes, inode, permissions, modification time and manifest evidence, refuse with the established diagnostic, and report no removal.

Measure the public portable prune and file-mode paths before and after; disk I/O is timed, installation fixture setup is excluded from the prune benchmark, and there are no network requests.

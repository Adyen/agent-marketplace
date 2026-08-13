# Issue: single-skill install fails in multi-registry configs

## Summary

`agent-marketplace install <skill>` currently writes to top-level `skills:`.  
In multi-registry configurations, top-level `skills` are inherited by all registry entries, so resolution expects the installed skill to exist in every configured registry.

This causes install failures when the skill exists in one registry but not others.

## Reproduction

Given:

```yaml
registries:
  - url: git@host:org/registry-a.git
    ref: main
  - url: git@host:org/registry-b.git
    ref: main
```

Where `alpha-tool` exists only in `registry-a`.

Run:

```bash
agent-marketplace install alpha-tool
```

Observed error:

```text
skill "alpha-tool" was selected but not found in <registry-b>@main
```

## Impact

- Single-skill install is unreliable in normal multi-registry setups.
- Users must manually edit config or temporarily remove registries.
- CLI behavior is unintuitive: command intent is “install this skill”, but config write implies “select in all registries”.

## Proposed change

For `install <skill>`:

1. Discover provider registries for the requested skill.
2. If exactly one provider exists, write to that registry entry (`registries[i].skills`) instead of top-level `skills`.
3. If multiple providers exist, fail with explicit disambiguation and require:
   - `install <skill> --registry <url>`
4. If no providers exist, keep existing not-found error.

Notes:

- Keep `install --tag` and `install --all` semantics unchanged.
- Keep top-level `skills` support for explicit global-default use cases.

## Acceptance criteria

- `install <skill>` succeeds in multi-registry config when provider is unique.
- Config write is registry-scoped, not top-level.
- Ambiguous providers return a clear error suggesting `--registry`.
- Existing error behavior for missing skills remains unchanged.
- Existing remove/sync behavior remains correct with registry-scoped installed skills.

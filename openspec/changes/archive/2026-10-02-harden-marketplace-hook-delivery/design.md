## Context

Native-hook-integration declares hook support roots and supplies opaque bases to
hookpolicy. Its application routing is unfinished. Marketplace support delivery
must use that seam rather than insert plugin tokens into compiled JSON.

## Decisions

- Per-hook directories use a stable source-identity hash and the installed hook
  name under the target layout's support root. Adjacent assets retain their
  `hooks/` plugin-relative prefix.
- Mirror declarations are acquisition-specific authority. Index-only declarations
  are refused. Materialization reads through no-follow descriptors, checks modes
  and digests on the bytes used in output, and keeps prior entries on refusal.
- Portable ownership state tracks installed regular support files even when other
  resources use symlink mode. Deselection removes only proven owned files.
- Hookpolicy handles shell quoting. Safe simple substitutions retain their
  historical output; literal path text never becomes shell substitution.
- The existing adjacent flat-file policy stays unchanged. Nested directories,
  non-hooks directories, and dynamic plugin-root expressions get explicit
  publication diagnostics and a stable unsupported refusal. Git remains the
  remedy; refreshing cannot satisfy expressions outside this policy.
- Deterministic unsupported/missing refusals retain verified archive evidence.
  Integrity failures invalidate it.

## Risks

The parent feature branch has known ownership/migration failures and unbound
scenarios. Validation must report those separately, not call the stack green.

## Verification

Affected packages pass apart from three materializer failures reproduced on
unchanged parent c4d3ccce. Scoped `make validate` passes with those three explicitly
excluded, with zero lint issues. The public Marketplace process gate and all
seven mirrored hook targets in both scopes pass, including execution after cache
deletion, deselection cleanup, and two independent same-named scripts.

The combined E2E gate has twelve failing scenarios, all reproduced with witnessed
execution on the unchanged parent: four Cursor compatibility scenarios, three
subagent scenarios, four Gemini native-MCP scenarios, and Windsurf scope
conversion. Spec tracing has the same 49 parent findings and no new findings.
Shellcheck passes. Vendor-native and TUI gates are not part of this change.

The warm local-disk materialization benchmark has five samples at two seconds
each. Descriptive medians: 786,508 to 1,823,857 ns/op (+131.89%), 192,636 to
339,161 B/op (+76.06%), and 1,829 to 2,943 allocs/op (+60.91%). The new path checks
source integrity and installed ownership and maintains regular installed support
rather than relying on executable cache links. This is a measured safety and
durability cost, not an optimization. Benchstat installation was blocked by an
HTTP 403 from the configured module proxy; no significance claim is made.

The adversarial review covered consumer reachability, failure preservation,
unintended writes, scopes, declaration provenance, shell literal substitution,
and regression validity. It found an additional missing-destination-ownership
hazard, which now refuses before aggregate mutation. Refusal and shell tests fail
on the pre-review snapshot 5ad80e0c and pass with these changes. No further
high-confidence introduced findings were identified in the bounded review;
that is not proof of defect absence.

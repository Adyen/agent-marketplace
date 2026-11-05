## Decision

Refuse rather than infer ownership. A portable proof scheme would change the
trust model, while warning only after deselection would still leave an active
hook behind a successful removal.

## Preflight

Compare the resolved selections before and after the proposed configuration
edit, including collection expansion and registry removal. Project hooks using
the existing verified source and harness-target catalogs. For native entries
that cease to be desired, inspect the actual native aggregate through the
existing descriptor-anchored reader and classify current values using private
ownership state.

An active matching entry without clean ownership proof refuses the operation
with `hook.remove-unowned`. Full native entry equivalence is used only to
identify residue, never to authorize deletion. A remaining selected contributor
to the same native entry prevents a false refusal.

The check precedes configuration and acquisition-lock publication and native
materialization. It does not write ownership state, adopt entries, replay
ownership journals or quote executable hook payloads. Existing dependency
recovery remains separate.

## Scope And Limits

This change protects `remove`, including collection and source removal, on the
seven targets with documented native hook aggregate projections. Targets with
file-based declarations, such as OpenCode, keep their existing file-ownership
and materialization path. A missing aggregate projection is not an unreadable
native file and must not activate or refuse the aggregate preflight.

It does not change ordinary sync of manually edited
configuration or make independent source-derived satisfaction into ownership.
Users can remove the entry deliberately after reviewing the named file, or
use established explicit ownership repair before retrying removal.

## Verification

Prove refusal through real second checkouts and separate HOME/state roots.
Check unchanged configuration, acquisition locks and native files, no success
message, and actionable value-free output. Cover handwritten exact matches,
missing/corrupt state, shared contributors, target/scope isolation, absent
native output and successful same-owner removal.
Also prove that active OpenCode declaration and adapter files neither activate
nor fail the aggregate preflight, and remain unchanged by its read-only check.

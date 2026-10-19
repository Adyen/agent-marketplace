# ADR 0009: Accept the SDK quarantine deletion boundary

**Status:** Accepted (2026-10-08)

## Decision

Keep the existing SDK stale-file pruning checks and explicitly exclude deliberate interference by another process running with the same OS user ID and write access to the quarantine directory during the final verification/deletion window. Do not claim atomic conditional deletion or guaranteed preservation in that window.

The normative contract is the [materialization specification](../../openspec/specs/materialization/spec.md#requirement-sdk-quarantined-payload-removal-retains-admitted-payload-authority). This decision clarifies the existing implementation; it adds no runtime change and removes no safety check.

## Context

Pruning moves an obsolete file to a randomly named quarantine entry, verifies its identity and bytes, and verifies it again immediately before deleting the entry. Replacements detected by those checks cause a stale-prune conflict and are preserved.

The final check and deletion are separate operations. On the supported Linux and macOS deletion path, deleting a name cannot be made conditional on that name still identifying the verified file. A same-account process with the necessary directory access can discover and replace the quarantine entry in the remaining window. Deletion can then remove the replacement instead.

In plain terms: we check a rubbish bag before throwing it away, but cannot stop someone with access to the bag from swapping it between those actions.

## Trade-off

The residual data-loss risk is real. A replacement removed in this window is not guaranteed to be recoverable. We accept that risk rather than add mechanisms that increase complexity without establishing the missing preservation guarantee.

In the ordinary local CLI deployment, an unrestricted same-account process with write access to the affected directory can generally already delete entries there directly. This race does not by itself demonstrate additional authority over those files. Same user ID alone is not proof of equal authority: a concrete sandbox or permission arrangement that gives an attacker less authority than the SDK would need separate analysis.

This is a narrow exclusion, not a blanket exemption for concurrent edits, untrusted registry content, symlink escapes, changed generations, or ownership failures. Existing refusal and preservation checks remain required. It is also not a promise to preserve every edit occurring after the final observation.

## Alternatives considered

| Alternative | Benefit | Limitation |
|---|---|---|
| Retain the verified file descriptor and inspect link count after deletion | Can detect some unexpected outcomes | Detection happens after the replacement may have been deleted, does not identify or restore its bytes, and does not prove every replacement was detected |
| Use a fresh owner-only (`0700`) quarantine directory | Reduces access by other accounts | Does not exclude a same-account process with access; adds directory creation, cleanup, and recovery work without closing this boundary |
| Add more verification calls | Can detect more changes before deletion | Still leaves a final observation/deletion gap |
| Use platform-specific primitives | Could help if their actual guarantees establish conditional deletion | The current supported path has no such guarantee; an atomic rename is not an atomic identity-conditional unlink |
| Introduce OS-enforced isolation | Can deny hostile processes access to the quarantine namespace | Requires a different deployment/security design, such as a separate security principal or sandbox with enforced access restrictions; it is not a small pruning fix |

## Consequences and verification

- Keep captured-parent authority, private random naming, bounded no-follow verification, changed-generation refusal, truthful removal receipts, and existing cleanup/recovery behavior.
- Record this as an accepted specification boundary, not a repaired race or a finding disproved by passing tests.
- Keep `TestSDKQuarantineRemovalPreservesReplacementGeneration` bound to the existing scenario. It replaces the entry before final verification and proves refusal, preservation, and healthy subsequent pruning. It does not exercise or prove preservation in the excluded final window.
- Reopen the decision if a concrete consumer or deployment demonstrates that the gap crosses an enforced authority boundary, or if a tested mitigation establishes stronger preservation on both supported platforms.

## References

- [Issue #580: quarantine verification/deletion decision](https://github.com/Adyen/agent-marketplace/issues/580)
- [Issue #431: filesystem security delivery plan](https://github.com/Adyen/agent-marketplace/issues/431)

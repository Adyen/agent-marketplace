## Context

State loading wraps read refusals in the state-corrupt diagnostic. Recover ignores that diagnostic unless root mismatch, so unsafe metadata and ordinary JSON corruption are conflated before recovery mutates evidence.

## Decisions

One private errMetadataRead marker on shared observed-reader errors, after optional absence normalization and deferred cleanup. Mark filesystem read/admission/generation/close failures only; JSON/record decoding happens outside this reader and retains its established recovery semantics. Recover treats marker/root mismatch as fatal before phase dispatch. Keep cause errors.Is, existing public codes/JSON/missing distinction, mandatory backup/kernel locking unchanged. No added/removed source comments, dependency or new byte budget. Original action-generation leases remain separate.

## Verification

Actual Apply commits gen1 and prepares gen2 then crashes after staging. Replace state with foreign alias: old Recover successfully removes the genuine stage/journal/red; corrected reader refusal reaches Recover and preserves native/prior/replacement/original/state/journal/stage/green. Restoring original permits genuine rollback/removal. Ordinary malformed regular-state control still rolls back using real journal authority and preserves malformed bytes, proving marker is not all state corruption. Linux/macOS ownership/materialize/count20 recovery/crash/validate/lint/build/strict spec and full file review pass. Nine implementation files reviewed incl test/script/spec exclusions, none skipped; cleanup marker deferred after normalization and actual consumer propagation checked. Existing public Load disk decoding lifecycle630.753→753.937µs,p=.056 n5/no95CI;41748B/336alloc unchanged. Variation/cost disclosed, no latency improvement claim.

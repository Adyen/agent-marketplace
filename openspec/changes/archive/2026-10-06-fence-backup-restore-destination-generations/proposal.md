## Why

Regular restore checks original destination/absence and ancestry before later validators/publication work. Replacing the destination at final publication overwrites foreign bytes and reports success. Moving those checks alone still accepts a replaced named parent because descriptor authority can remain valid under a detached original directory.

## What Changes

- Move existing original destination/absence and captured ancestry checks to the final publication boundary after validators and completed staged-state verification.
- Match the named output parent to captured parent identity before relative rename, reusing existing directory admission without changing initial alias policy.
- Preserve original/foreign bytes and metadata, admitted absence, owned-stage cleanup and real restore mode behavior.

## Capabilities

### Modified Capabilities

- `backup-recovery`: original destination/absence and named parent generations remain valid at final regular restore action.

## Impact

R2 child of #565, stack #560; shared ordinary/mapped/Codex restore publisher only. Existing safety checks reordered plus existing named-directory identity admission reused; no new helper/type/API/dependency/budget/source-comment, content policy or durability sync. Symbolic deletion/remaining consumers and final audit remain separate. Not a whole-operation transaction or atomic namespace CAS against uncooperative writers.

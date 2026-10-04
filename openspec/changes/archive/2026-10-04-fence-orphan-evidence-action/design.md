## Context

Re-nomination protects preview-to-action semantics, but fresh evidence becomes plain claims before backup/staging/policy validation. Its original opened metadata identity was discarded, so a late alias or identical-JSON replacement still authorized actual deletion.

## Decisions

Preserve manifest file pin privately in returned RetainedBackup; keep parent device/inode only so harmless staging timestamps do not change returned records. Existing journal observed pin and manifest pin travel privately through Evidence, with source store and strict relative metadata location. Fresh matching candidate is selected under the existing write session. Apply's policy callback first executes existing policy validation, then candidate ValidateEvidence reopens scoped metadata through the shared regular/single-link reader and requires original file state and parent object. Existing CAS refusal cleanup owns attempt stage/journal/backup removal. No JSON/source-comment changes, added dependency, new limits, cross-invocation consent or global transaction. Zero constructed candidate refuses without authority.

## Verification

Actual native sync creates managed entry and real retained evidence; remove durable state and actual public repair preview nominates orphan. During public RemoveOrphan after real staged write, replace authorizing manifest with external alias: parent deleted native entry/red; fix returns typed unsafe-path/no publication and preserves native/foreign/replacement evidence with no stage/journal. Both native Devin/Codex adapters test aliases and byte-identical regular replacement; restored real metadata permits real removal with fresh mandatory backup. Existing policy-refusal/retained-backup controls, Linux/macOS full ownership/materialize/count20 action-policy races/validate/spec pass. Whole public RemoveOrphan disk lifecycle71.537447→75.153009ms,p=.310 n5/no95CI;314401→341555B(+8.64%),4203→4535alloc(+7.90%). Setup sync/preview excluded, actual lock/read/evidence/backup/stage/state/journal/delete/cleanup included; no network/logs discarded both. Variable loaded111–138ms samples retained, no speedup or wall-budget claim.

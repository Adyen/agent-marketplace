## Implementation

- [x] Public Apply state-stage same-JSON replacement overwrites original state before post-rename conflict red/green.
- [x] Existing parent/original-generation and captured staged-reader checks moved to final publication; real state/prepared-journal/journal-phase/safety-manifest preservation and actual recovery/retry controls pass.
- [x] Linux/macOS ownership/materialization/macCLI, full ownership, fresh-cache validate/lint/build and20-run Apply/Metadata/BackupOnly/PublishedMetadata/PublishCrossed/Recover/Safety/ReadJournal/Load races/repeats, strict specaudit/shellcheck. New test tagged-switch lint repair verified without weakening gate.
- [x] All8 intended files host-reviewed,0 skips. Clean serial frozen d83c baseline/final existing metadata publication disk2s/count5:11.11→10.77ms(p=.841,n5), B/op+1.58%,alloc208→211(+1.44%). Earlier validation-overlap sample discarded; finite-sample/host caveats/no95%CI or general speedup claim, validation costs retained/disclosed.
- [x] Archive as final commit.

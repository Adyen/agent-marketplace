# Tasks

## 1. Selection recovery

- [x] 1.1 Preserve the complete `deployed-acquisition-locks` requirement in the delta and add the effective-selection rule and shadowed-fragment scenario; verify the requirement and all scenarios are present in the delta.
- [x] 1.2 Bind the existing shadowed-fragment regression test to the new scenario; verify the annotation is directly above its `t.Run`.

## 2. Removal evidence

- [x] 2.1 Preserve the complete `resource-removal` requirement in the delta and add the unrelated-hook rule and both scenarios; verify the requirement and all scenarios are present in the delta.
- [x] 2.2 Bind the existing non-hook removal and hook-refusal tests to their new scenarios; verify each annotation is directly above its test.

## Workflow follow-up

- [x] Validate the change and its bindings with strict OpenSpec validation and specaudit trace.
- [x] openspec archive preserve-marketplace-recovery-boundaries --yes

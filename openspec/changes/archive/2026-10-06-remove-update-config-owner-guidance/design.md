## Context

Organization-managed Marketplace pins are counted without checking for newer revisions. The output currently follows that summary with an instruction to contact the organization config owner.

## Decisions

Delete the instruction without replacing it. Keep the summary and verbose details so consumers can see which pins were not checked. No dependency or materialization behavior changes.

## Validation

Assert that default output retains the summary and verbose hint without the owner instruction. Exercise verbose output through the update command and verify that the pin remains listed and the organization acquisition lock remains absent.

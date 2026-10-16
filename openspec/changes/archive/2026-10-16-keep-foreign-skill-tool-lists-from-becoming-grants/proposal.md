## Why

Claude, Cursor and Gemini read a skill's `allowed-tools` as a pre-approval: listed tools run without a permission prompt and nothing is restricted. Other harnesses give the field a different meaning. Devin's skill `allowed-tools` is an auto-approval list in Devin's own tool vocabulary, and Factory's skill `allowed-tools` is review metadata that grants nothing. Conversion copied those lists verbatim, so a Factory list naming `Read` and `Edit` became a real Claude pre-approval for edits (#637).

## What Changes

- Claude, Cursor and Gemini compilation omit a skill `allowed-tools` list that carries another harness's tool provenance, with the new warning `skill.allowed-tools-omitted`. The skill is still emitted.
- Lists without provenance (canonical Claude vocabulary) and lists from the target's own harness are unchanged.

## Capabilities

### Modified Capabilities

- `harness-compilation`: foreign skill tool lists never become pre-approvals.

## Impact

Claude, Cursor and Gemini skill compilers and one new warning code. A converted Devin, Factory or Gemini skill on another target loses its pre-approval list and runs its tools through the target's normal permission prompts. Subagent tool restrictions are unaffected.

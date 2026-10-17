## Why

#719 stopped foreign skill `allowed-tools` lists from becoming Claude, Cursor or Gemini pre-approvals, but converted commands carry no tool provenance. Devin command ingestion canonicalizes its auto-approval list to Claude IDs, so a Devin command reached Claude as `allowed-tools: [Read, Bash]`; Factory command metadata reached it verbatim (#637, maintainer decision to cover commands).

## What Changes

- `ir.Command` gains `ToolSource`, the harness whose `allowed-tools` extension list the command carries; empty means canonical. Devin, Factory and Gemini command ingestion set it when the command declares `allowed-tools`.
- The Marketplace resource payload carries a command's `ToolSource`, and an extension field cannot supply it. The frozen source-revision fallback digest ignores it, like skill and subagent provenance.
- Claude, Cursor and Gemini omit a foreign command `allowed-tools` list with the existing warning `skill.allowed-tools-omitted`.

## Capabilities

### Modified Capabilities

- `harness-compilation`: foreign skill tool lists never become pre-approvals, now including commands.

## Impact

Command IR, the Devin, Factory and Gemini command ingesters, the canonical artifact encoder, and the Claude, Cursor and Gemini command compilers. Command payloads without provenance are byte-identical. An older client decoding a payload with command `ToolSource` keeps it as an unknown extension field.

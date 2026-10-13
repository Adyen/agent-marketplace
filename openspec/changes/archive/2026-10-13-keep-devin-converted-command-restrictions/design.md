## Context

The Devin target has no command primitive. A command compiles to a converted command skill: `skills/<name>/SKILL.md` with `x-agent-marketplace-kind: command`, `triggers: [user]` and an optional strictly mapped `allowed-tools` list. The ingester reads the marker back as command provenance.

The command IR holds a name, description, body and extension fields. It cannot hold a `permissions` policy, an execution selector, or activation other than the user-only activation its compiler writes. Devin itself honors those fields on the skill file, so dropping them while recovering a command changes what the resource may do or who may invoke it. The Codex ingester already refuses marked commands carrying authority-bearing skill controls.

On the compile side, the Devin command compiler reserves `permissions` and the provenance marker but passes every other extension field through, including `agent` and `subagent`. The Devin ingester already refuses to carry those two fields into the command IR because they are unvalidated execution targets.

## Decisions

1. **Refuse instead of dropping at ingestion.** A marked skill that declares `permissions`, `agent`, `subagent`, or a trigger list without `user` is refused with a new error, `ingest.devin.command-security-unsupported`. It follows the existing `ingest.devin.tool-unreversible` and Codex marked-command precedents: ingestion writes the harness-neutral IR every compiler reads, so a dropped control would propagate to every target. The diagnostic names fields, never values.
2. **Keep narrowing drops as warnings.** `model`, and extra activation next to `user` (for example `[user, model]`), still drop with `ingest.devin.command-field-dropped`; the compiler-written `triggers: [user]` and absent triggers are consumed silently. A converted command is user-only, so these only narrow it. When an unreversible `allowed-tools` entry already refuses a marked skill, the control refusal is reported alongside it.
3. **Reserve execution selectors at compilation.** `agent` and `subagent` join `permissions` as unconditionally reserved command keys, using the existing `compile.canonical-field-overrides-extra` warning. The converted command skill runs under its own mapped `allowed-tools` restriction, and an extension field cannot route it to another agent.
4. **Leave ordinary skills alone.** Typed `agent`/`subagent` on an ordinary skill remain native Devin fields and keep round-tripping.

## Non-goals

Other harness targets, the unrepresentable Devin `allowed-tools` omission policy, model mapping, and the treatment of extension fields on other command compilers.

## Verification

Public `Convert` regressions fail before the change: a marked Devin skill with a deny policy, model-only activation, or an execution selector is recovered as a command and emitted without the control; a command with an `agent` extension compiles to a Devin skill that carries it. After the change they refuse or reserve, and positive controls show that a plain converted command skill and an ordinary skill with selectors still round-trip byte for byte, and that user-and-model activation narrows to a converted command with a warning.

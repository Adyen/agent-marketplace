## Context

`agent-marketplace convert` ran `pkg/harness-compilation` `Convert` over a captured source and then published the result with `Materialize`, with extra Codex and Devin publication logic for shared configuration files. Nothing in the Marketplace sync, install, or lock workflows calls it.

## Decisions

- Delete the command and every helper only it used, including the conversion publisher and the secure-unix source boundary. Shared helpers stay when other commands use them.
- Keep harness translation available only through the library. Its Convert and Materialize contracts are unchanged.
- Port the convert-based E2E scenarios to call `hcc.Convert` and `hcc.Materialize` directly. Refusal, partial-result, and clean outcomes keep their existing assertions. Codex shared-configuration output is asserted at the artifact level, because `Materialize` refuses externally reconciled shared files without the removed publisher.
- Keep the `convert` capability label reported by `doctor` and `status`.

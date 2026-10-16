## Context

See proposal.md for motivation. The app is layer 4 of the stacked delivery and depends on `interactive-session-core` and `gui-local-api`. The GUI is user-scope-only: the user-personal root is writable, and the Consumer organization configuration is read-only. The earlier web-to-`marketplace-ui/` rename is a separate PR and is not part of this change.

## Goals / Non-Goals

**Goals:** Present the session core through an accessible desktop shell, retain existing user configuration boundaries, and align first-run and structured mutations with their existing CLI equivalents.

**Non-Goals:** Repository or personal-local scope, raw configuration editing, CLI-only doctor/migrate-command/gc/backup operations, Windows support, update installation, or file watching.

## Decisions

- **Host and frontend:** Use a separate cgo Wails host in `cmd/agent-marketplace-gui`; keep the CLI `CGO_ENABLED=0`. Select Wails v2 when its AssetServer handler streams SSE on both platforms. If it does not, carry the `gui-local-api` spike's recorded decision to adopt Wails v3 or use the loopback socket into this host design before implementation. Build the Vue 3 + Vite frontend in `gui/`, using `@nuxt/ui` and Tailwind tokens consistent with the Marketplace UI.
- **Screens:** Provide a resource list grouped by status, detail using the inspection projection, install/remove confirmation, sync/update with progress, status/drift, guided first run, and settings for Harness targets and sources. Show a read-only “managed outside the GUI” panel for configuration fields outside these forms.
- **Workspace boundary:** Resolve only the user-personal root and read-only Consumer organization configuration; do not inspect repository or personal-local configuration and do not derive behavior from the current working directory.
- **First run:** Show setup only when the user-personal root is absent. Preselect detected Harness targets, require at least one, and mirror interactive `agent-marketplace init`'s Community telemetry consent condition. The GUI consent choice has no preselected answer and requires an explicit choice; an affirmative choice enables basic telemetry and a negative choice declines. The interactive CLI's empty response defaults to affirmative, while the non-interactive `init --harnesses` path does not prompt. Hold the GUI's first-run values until setup completes so cancellation writes nothing. Compare the completed Harness/consent configuration bytes with interactive `agent-marketplace init`; exclude optional source entries, which are added afterward through the `gui-configuration` source mutation path. `init` creates only the v4 root/Harness selection and does not add registries or configure a Marketplace.
- **Organization version-check policy:** Honor `gui.version_check` only from Consumer organization configuration; default to enabled. When the organization sets it to false, make no release-availability network request and show no notice. Use the project's public GitHub Releases latest stable release as the source.
- **CLI mutation parity:** Add no CLI commands. For byte-parity cases, use only existing command surfaces:

  | GUI edit | Existing CLI equivalent |
  |---|---|
  | Initial Harness target selection | `agent-marketplace init --harnesses <targets>` creates the user-personal root; interactive `agent-marketplace init` is the consent/output comparison. |
  | Edit Harness targets on an existing root | `agent-marketplace tui`, then `H` and the user/global scope selection. |
  | Add a Marketplace endpoint registration | `agent-marketplace install <Marketplace endpoint> --scope user`. |
  | Remove a Git/directory registry entry | `agent-marketplace remove --from <URL> --scope user` without a Resource name. |

  Compare the GUI and CLI resulting bytes from copies of the same initial root for those equivalent operations. For source-only Git/directory additions and Marketplace endpoint registration removal, there is no equivalent CLI command: do not add one. Instead, verify the effective configuration changes only by the added/removed entry and every included fragment remains byte-identical. `agent-marketplace install --from <URL>` is not an equivalent source-only add because it also installs Resource(s); the TUI `E` action is a raw editor, not a structured mutation.
- **Configuration rollout:** The v4 reader currently reports unknown top-level fields as `v4.unknown-field` in `internal/config/v4.go`; the strict legacy reader uses `KnownFields(true)` in `internal/config/v3.go`. Older clients would reject organization config containing `gui`. Ship CLI read support first. Release notes and administrator documentation SHALL instruct operators to deploy `gui.version_check` only after every managed client runs a release that reads it.
- **Reloads:** An explicit refresh, window-focus regain, and completion of the GUI's own mutation trigger session reloads. Focus regain is an ordinary reload; retain the accepted view until final assessment per `session-startup`. Do not add a file watcher.
- **Accessibility and language:** Expose every action through a visible control and keyboard operation; provide accessible names and English-only text. Test the keyboard path and accessible names independently of visual rendering.
- **Telemetry:** Use `gui` as the command-surface attribute and preserve TUI action/view/Resource-kind event semantics, the existing Telemetry level, Community telemetry consent, and Organization telemetry policy.
- **Confirmation parity:** Implement the `session-interactions` migration, sync-and-quit, and quit confirmations in the GUI. For example, closing the window while sync is pending SHALL offer the same affirmative, negative, and cancel choices as the TUI.
- **Verification:** Use Go host tests, Vitest component tests, and Playwright against the loopback API. Add GUI annotations for every `session-*` scenario, paired with TUI-surface witnesses. Enforce the pairing in `tools/specaudit`.

## Risks / Trade-offs

- [Configuration serialization diverges from an equivalent CLI mutation] → Compare byte-for-byte outputs from the same starting user-personal root for every operation with an existing CLI equivalent. For the two operations without CLI equivalents, compare effective configuration deltas and included-fragment bytes as specified.
- [An older managed client reads `gui.version_check` before its configuration decoder is updated] → Ship CLI read support and the administrator rollout notice before enabling the key in Consumer organization configuration.
- [A focus reload loses an in-progress view or misreports drift] → Treat focus regain as an ordinary session reload and test that the accepted list remains until final assessment.
- [Accessibility is met by visual affordances only] → Exercise every action by keyboard and assert accessible names in frontend tests.

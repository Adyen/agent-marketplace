# Harness validation

This directory documents how Agent Marketplace validates each supported **harness target**. It is the contributor guide for adding a new target or extending the validation of an existing one.

## Two validation layers

Validation deliberately has two layers:

1. **Compiler and configuration validation** runs without vendor executables. The existing harness-compilation matrix covers every source-to-target pair for skills, commands, subagents, hooks, and MCP servers. The Agent Marketplace compatibility driver additionally exercises `list`, collection expansion, `sync`, generated paths, materialization mode, scope isolation, rules, and expected diagnostics.
2. **Native CLI validation** runs only in GitHub Actions. It uses a pinned, repository-owned Linux image, an isolated HOME and workspace, no credentials, no model calls, no external MCP services, and `--network none`. The test asks the vendor CLI to list or inspect generated resources where a credential-free noninteractive command exists.

Native validation is informational on pull requests. It must still report real failures. A missing binary, failed image build, timeout, authentication prompt, trust gate, or malformed configuration is not a successful skip.

## Common scenario

Each target scenario uses a fresh sandbox and a canonical local fixture registry outside the native discovery ancestry:

1. Create fixture resources: skill, command, subagent, hooks, two MCP servers, collection, and a rule source.
2. Assert the generated destinations are absent before installation.
3. Run Agent Marketplace catalog commands for every selectable kind.
4. Install the collection and run `sync`.
5. Assert native paths, complete fixture values, support files, aggregate preservation, scope isolation, and symlink/file mode.
6. Run `sync --check` to verify that validation did not create drift.
7. In native mode, run the target CLI version command and a negative discovery control before materialization.
8. Run the target's documented list/show/get command after materialization.
9. Record structured evidence without copying raw native output into reports.

The driver is `e2e/TestHarnessCompatibility`. The fixture and native command contracts are split across `e2e/harness_compatibility_fixture_test.go`, `e2e/harness_compatibility_native_test.go`, and `e2e/harness_compatibility_test.go`.

## Evidence status

| Status | Meaning |
|---|---|
| `native-verified` | The target CLI observed the named resource and every asserted field exposed by that command. |
| `config-only-verified` | Agent Marketplace generated and reconciled the expected native configuration, but no safe native inspection command exists for that resource. |
| `unsupported-as-expected` | A typed compiler or scope diagnostic intentionally suppresses the output. |
| `blocked` | A trust, authentication, setup, or other prerequisite prevented the native assertion. |
| `failed` | An assertion, configuration parse, materialization, or native inspection contract failed. |

MCP configuration recognition and transport connection are separate evidence. Codex and Factory currently verify configuration recognition; Claude and Gemini additionally require both local stdio fixture servers to report a connection after the isolated trust/approval setup.

## Trust and approval

Some CLIs suppress project resources until a workspace is trusted. When documented, CI tests both states:

- First, the untrusted fixture must show the expected disabled, pending, or absent behavior.
- Then CI writes trust state only inside the disposable fixture HOME and workspace, preserving existing settings and refusing symlink escapes.
- Native discovery is repeated and the final `sync --check` confirms that trust setup did not change Agent Marketplace output.

The application never grants trust as part of synchronization. Fixture trust is test setup only and is reported as `repository-only` evidence.

## Images and versioning

`.github/workflows/harness-images.yml` resolves the newest stable supported CLI and builds a target image. Published images are produced only from the default branch after an offline version smoke test. `.github/workflows/harness-compatibility.yml` pulls the image by its moving `ci` reference, verifies its recipe and Go-version labels, and uses the resulting immutable image ID. If no suitable image exists, the PR job builds an unpublished bootstrap image.

The image resolver and installer live under `tools/harness-compat/`. Do not install vendor CLIs or run native probes from ordinary local tests.

## Adding a harness target

1. Register the target in `pkg/harness-compilation/target` and define its native layout and capabilities.
2. Add an explicit row to `tools/harness-compat`'s capability map. Choose `probe` only when a credential-free noninteractive inspection command is verified; otherwise declare `unavailable` with a reason.
3. Add an image installation path, pinned package or verified bundle, version smoke test, and auto-update suppression.
4. Add expected repository and user paths to the fixture test using independently authored paths.
5. Add native probes with a negative control and assertions for names plus observable fields.
6. Document trust, authentication, connection, and unavailable inspection surfaces in the target guide.
7. Add regressions for malformed config, unmanaged collisions, scope leakage, idempotence, file/symlink mode, and expected unsupported diagnostics.
8. Keep native execution in GitHub Actions. Never make `go test ./...` depend on a third-party executable or real credentials.
9. Run `make harness-compat-check`, the relevant compiler/materializer tests, and the full GitHub workflow. Repeat the internal-domain and secret scan before publishing.

## Target guides

- [Claude](./claude.md)
- [Factory](./factory.md)
- [Cursor](./cursor.md)
- [Windsurf](./windsurf.md)
- [Gemini](./gemini.md)
- [Devin](./devin.md)
- [Codex](./codex.md)

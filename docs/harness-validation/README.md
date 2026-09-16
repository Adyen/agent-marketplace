# Harness validation

This is the contributor guide to validating every supported **harness target**. Start with the target guide, then follow the implementation links below.

## Principle: inspect every resource the native CLI can expose

Filesystem assertions and native discovery answer different questions. A correct-looking file is not evidence that its consumer loaded it. Every resource with a usable native inspection surface must have a native probe, including CLI stdio protocols used by vendor SDKs—not just standalone `list` subcommands.

The constraints are credential-free inspection, no model turns, and no external service dependencies. A genuinely unavailable surface needs a specific explanation. An implemented probe that times out, rejects configuration, or cannot authenticate stays a failure/blocker; it must not silently become a config-only pass.

[Probe definitions and explicit gaps](../../e2e/harness_compatibility_coverage_test.go) are checked against every registered harness and resource kind. A newly added target cannot silently inherit a blanket exemption. Collections are virtual selections, not native runtime primitives.

## Validation layers

1. **Compiler contracts:** the harness-compilation matrix exercises source-to-target conversion with independently authored expected artifacts and loss/omission diagnostics.
2. **Application contracts:** hermetic tests cover catalog selection, collection expansion, materialization, shared-file ownership, preservation, refusal, cleanup, and scope behavior. The compatibility scenario specifically checks the canonical fixture's generated paths and values in both materialization modes.
3. **Native inspection:** the installed vendor executable lists or inspects the actual output. No alternate resource directory, inline agent definition, or copied configuration may stand in for the generated files.

## Common scenario, in execution order

Each target runs four scenarios: `repo/symlink`, `repo/file`, `user/symlink`, and `user/file`.

1. Create a fresh temporary HOME, Git workspace, state/cache roots, and local registry outside discovery ancestry.
2. Seed a skill with support files, a command, a subagent, two harmless hooks, two local stdio MCP definitions, a collection, and a separate rule source.
3. Assert output destinations are absent. In native mode, verify the executable version and run each probe's **pre-install negative control**.
4. Run Agent Marketplace `list --kind` for skills, commands, subagents, hooks, MCP servers, and collections.
5. Install the fixture collection and run `sync`.
6. Assert paths, generated content, support files, materialization mode, and absence of fixture output in the opposite scope. Test rules separately through conversion/diagnostics.
7. Run `sync --check`.
8. Where applicable, observe the native trust/approval gate, then grant only the necessary fixture-local trust or server approval.
9. Run native post-install probes and inspect their actual inventories/fields.
10. Run `sync --check` again and assert that inspection did not execute a fixture hook.
11. Write structured evidence and attach a concrete limitation to resources that remain config-only.

The entry point is [TestHarnessCompatibility](../../e2e/harness_compatibility_test.go); [fixtures and expected paths](../../e2e/harness_compatibility_fixture_test.go) are separate from production target routing.

## CLI protocols are native validation

Codex `app-server`, Claude stream-JSON control requests, and Factory `droid exec` RPCs are subprocess interfaces to the vendor runtime, not replacement parsers. The driver uses its existing Go standard-library process support rather than adding a vendor SDK dependency.

The [protocol transport](../../e2e/harness_compatibility_protocol_test.go) performs only initialization and explicitly chosen metadata requests. It correlates response IDs, rejects malformed responses and unexpected server requests, bounds time/output, closes stdin, and reaps the process group. It never sends a prompt or model-turn request. Tests use internal fake executables to verify these boundaries; fake results are not native compatibility evidence.

The [inventory assertions](../../e2e/harness_compatibility_inventory_test.go) check entries in the correct returned collection, not arbitrary occurrences of a fixture name in JSON or error text. Expected identities, descriptions, paths, scopes, and handler fields remain independently specified.

## Reading results

| Result | Meaning |
|---|---|
| `native-verified` | The declared native inspection assertions passed. The `native-cli` setup row proves only the executable version, not resource acceptance. |
| `config-only-verified` | Generated configuration assertions passed; native execution was explicitly disabled or this resource has a documented inspection gap. |
| `unsupported-as-expected` | The compiler intentionally omitted output with the expected stable diagnostic. |
| `blocked` | A setup, authentication, or inspection prerequisite prevented a required probe. This fails the compatibility result. |
| `failed` | Materialization, native parsing, an inventory assertion, or a safety boundary failed. |

MCP recognition and transport evidence are separate. Claude and Gemini require two reported local connections; Cursor uses native `list-tools` responses. Codex, Devin, and Factory list/get checks do not by themselves certify a connection. `not-probed` never means connected.

Artifacts include `compatibility-<target>.json`, `summary.txt`, the test log, and image/release metadata. Command records retain arguments, exit status, bounded-output metrics, allowlisted diagnostics, and asserted observations—not arbitrary native output or credentials. Generated-file snapshots contain only the disposable fixture. PR artifacts expire; the target guides and source code define the continuing contract.

## Required merge gate

The workflow exposes one stable check named **Harness Compatibility**. Its `required` job runs with `always()` and depends on planning, compiler/driver contracts, and the entire harness matrix. It accepts only `success` for all three results; failure, cancellation, skipping, or a missing result fails the gate. The [workflow regression test](../../tools/harness-compat/workflow_test.go) executes that gate's actual shell command against successful and unsuccessful upstream results.

The repository's active `protected-main` ruleset requires this check from GitHub Actions, alongside the existing required checks. A workflow name alone is not a merge requirement: forks or newly configured repositories must also select the stable check in their own branch protection or ruleset. Do not require each matrix-job name individually, because adding a harness should automatically extend the aggregate gate.

Native regressions and image/setup failures now block merging. Documented config-only coverage and expected compiler omissions still count according to the tested contract; the gate does not turn them into native verification. Direct release-job dependencies and the image publication policy are unchanged.

When activating this requirement before the workflow reaches the default branch, other open PRs need the workflow update before they can satisfy the check. Do not bypass protection or weaken assertions to work around a missing or failing result.

## Isolation and trust

Native executables and containers run in GitHub Actions, not during ordinary local verification. The test container uses `--network none`, drops capabilities, enables `no-new-privileges`, and does not receive vendor credentials. Each subprocess uses an allowlisted environment with isolated HOME/XDG/Codex roots and sanitized Git settings.

Trust belongs to the execution workspace, not to a resource's installation scope. For example, Gemini suppresses user-level MCP connections in an untrusted workspace. Native CI enables `HARNESS_COMPAT_TRUST_FIXTURE=1`; the [trust helper](../../e2e/harness_compatibility_trust_test.go) confines writes to a real disposable repository/home pair. Claude and Cursor approval is restricted to the two fixture MCP names; no broad approval switch is used.

These are test setup actions. Agent Marketplace synchronization never grants workspace trust or server approval. Hook discovery is not hook execution.

## Images and versions

[Daily image publishing](../../.github/workflows/harness-images.yml) builds Linux/amd64 images daily and on manual dispatch from the default branch. It resolves newest stable releases immediately without changing the repository's normal dependency-age policy. Only trusted default-branch jobs publish to GHCR after an offline version smoke check. Version/run tags are immutable by convention; the moving `ci` tag advances only after that smoke check, independently of compiler compatibility results. Compatibility results do not prevent a newly built, runnable CLI image from surfacing upstream regressions.

[PR compatibility](../../.github/workflows/harness-compatibility.yml) verifies target, recipe, and Go-version labels before reusing a published image. It executes the immutable image ID, or builds an unpublished candidate when an image is missing or the recipe changed. Reports show the source, actual version, image identity, age, and stale-image warning.

Release sources differ: npm uses stable package metadata; Devin supplies a bundle manifest with SHA256; Cursor's public installer identifies a calendar-versioned bundle. Cursor installer text is parsed, never executed. Its downloaded bundle digest is recorded, not represented as a publisher-provided checksum. See [release resolution](../../tools/harness-compat/resolve.go), [installation](../../tools/harness-compat/install.sh), and [image construction](../../tools/harness-compat/build-image.sh).

## Adding or extending a harness

1. Implement/register the target's compiler, ingester, layout, and diagnostics independently of the validation driver.
2. Audit **every resource kind** against current CLI help, official protocol documentation, and source where available. Session lists are not agent-definition lists; SDK-provided callbacks are not installed-hook inventories.
3. Register the executable/release source in [the image capability map](../../tools/harness-compat/main.go), or an explicit reason why no native executable can be tested.
4. Add independently authored expected fixture paths, including split roots and scope overrides.
5. Add a probe for every usable surface in `compatibilityResourceProbes`. Give each remaining kind an explicit limitation in `compatibilityNativeGap`; update the target guide with evidence.
6. Add negative controls, actual-entry/field assertions, malformed-response checks, and lifecycle/credential boundaries. Do not certify a resource merely because the process exited zero.
7. Keep trust/approval fixture-local and distinguish discovery, connection, and execution. Do not inject substitute resource definitions through protocol options.
8. Run local compiler/materializer regressions and the commands below, then validate the actual executable in CI. Record new native findings instead of suppressing them to obtain green checks.
9. Update this directory and audit changed fixtures/reports for private infrastructure identifiers and credentials before pushing.

## Local verification

```bash
make harness-compat-check
make harness-compat-config
```

The first checks metadata, helper/transport boundaries, and shell scripts without vendor executables. The second runs all target/scope/mode fixture combinations in explicit configuration-only mode. Neither is a substitute for native CI. `HARNESS_COMPAT_TARGET` being unset keeps ordinary E2E runs from invoking vendor CLIs. `HARNESS_COMPAT_REPORT_DIR` selects where the driver retains structured evidence.

## Target guides

- [Claude](claude.md)
- [Factory](factory.md)
- [Cursor](cursor.md)
- [Windsurf](windsurf.md)
- [Gemini](gemini.md)
- [Devin](devin.md)
- [Codex](codex.md)

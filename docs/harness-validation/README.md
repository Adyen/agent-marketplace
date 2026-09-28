# Harness validation

This contributor guide explains how every supported harness target is
validated: `claude`, `codex`, `cursor`, `devin`, `factory`, `gemini`, and
`windsurf` (the registry in `pkg/harness-compilation/target`). Start with the
target guide, then follow the implementation links below.

## Principle: inspect every resource the native CLI can expose

Filesystem assertions and native discovery answer different questions: a
file can look correct without its consumer ever loading it. Every resource
with a usable native inspection surface must have a native probe. That
includes CLI stdio protocols used by vendor SDKs as well as standalone `list`
subcommands.

Probes inspect without credentials, run no model turns, and depend on no
external service. A surface that is truly unavailable needs a specific
explanation. An implemented probe that times out, rejects configuration, or
cannot authenticate stays a failure or blocker; it must never turn silently
into a config-only pass.

The [probe definitions and explicit gaps](../../e2e/harness_compatibility_coverage_test.go)
are checked against every registered harness and resource kind, so a newly
added target cannot inherit a blanket exemption. Collections are virtual
selections, not native runtime primitives.

## Validation layers

1. Compiler contracts: the harness-compilation matrix exercises
   source-to-target conversion against independently authored expected
   artifacts and loss/omission diagnostics.
2. Application contracts: hermetic tests cover catalog selection, collection
   expansion, materialization, shared-file ownership, preservation, refusal,
   cleanup, and scope behavior. The compatibility scenario checks the
   canonical fixture's generated paths and values in both materialization
   modes.
3. Native inspection: the installed vendor executable lists or inspects the
   actual output. No alternate resource directory, inline agent definition, or
   copied configuration may stand in for the generated files.

## Common scenario, in execution order

Each target runs four scenarios: `repo/symlink`, `repo/file`, `user/symlink`,
and `user/file`.

1. Create a fresh temporary HOME, Git workspace, state and cache roots, and a
   local registry outside the discovery ancestry.
2. Seed a skill with support files, a command, a subagent, two harmless hooks,
   two local stdio MCP definitions, a collection, and a separate rule source.
3. Assert that output destinations are absent. In native mode, verify the
   executable version and run each probe's pre-install negative control.
4. Run Agent Marketplace `list --kind` for skills, commands, subagents, hooks,
   MCP servers, and collections.
5. Install the fixture collection and run `sync`.
6. Assert paths, generated content, support files, materialization mode, and
   that no fixture output appears in the opposite scope. Rules are tested
   separately through conversion and diagnostics.
7. Run `sync --check`.
8. Where the harness has one, observe the native trust or approval gate, then
   grant only the fixture-local trust or server approval that is needed.
9. Run the native post-install probes and inspect their actual inventories
   and fields.
10. Run `sync --check` again and assert that inspection did not execute a
    fixture hook.
11. Write structured evidence, and attach a concrete limitation to each
    resource that remains config-only.

The entry point is [TestHarnessCompatibility](../../e2e/harness_compatibility_test.go).
The [fixtures and expected paths](../../e2e/harness_compatibility_fixture_test.go)
are kept separate from production target routing.

## CLI protocols are native validation

Codex `app-server`, Claude stream-JSON control requests, and Factory
`droid exec` RPCs are subprocess interfaces to the vendor runtime. They are
not replacement parsers. The driver talks to them with Go standard-library
process support and adds no vendor SDK dependency.

The [protocol transport](../../e2e/harness_compatibility_protocol_test.go)
sends only initialization and explicitly chosen metadata requests. It
correlates response IDs, rejects malformed responses and unexpected server
requests, bounds time and output, closes stdin, and reaps the process group.
It never sends a prompt or a model-turn request. Tests check these boundaries
with internal fake executables; fake results are not native compatibility
evidence.

The [inventory assertions](../../e2e/harness_compatibility_inventory_test.go)
look for entries in the correct returned collection. A fixture name that
appears elsewhere in JSON or in error text does not count. Expected
identities, descriptions, paths, scopes, and handler fields are specified
independently of the code under test.

## Reading results

| Result | Meaning |
|---|---|
| `native-verified` | The declared native inspection assertions passed. The `native-cli` setup row proves only the executable version, not resource acceptance. |
| `config-only-verified` | Generated configuration assertions passed; native execution was explicitly disabled or this resource has a documented inspection gap. |
| `unsupported-as-expected` | The compiler intentionally omitted output with the expected stable diagnostic. |
| `blocked` | A setup, authentication, or inspection prerequisite prevented a required probe. This fails the compatibility result. |
| `failed` | Materialization, native parsing, an inventory assertion, or a safety boundary failed. |

MCP recognition and transport evidence are recorded separately. Claude and
Gemini must report two local connections, and Cursor uses native `list-tools`
responses. The Codex, Devin, and Factory list/get checks do not certify a
connection on their own. A connection status of `not-probed` never means
connected.

Artifacts include `compatibility-<target>.json`, `summary.txt`, the test log,
and image and release metadata. Command records keep arguments, exit status,
bounded-output metrics, allowlisted diagnostics, and asserted observations.
They do not keep arbitrary native output or credentials. Generated-file
snapshots contain only the disposable fixture. PR artifacts expire after 7
days; the target guides and the source code define the lasting contract.

## Required merge gate

The [Harness Compatibility workflow](../../.github/workflows/harness-compatibility.yml)
exposes one stable check named "Harness Compatibility". Its `required` job
runs with `always()` and depends on planning, the compiler and driver
contracts, and the whole harness matrix. `plan` must succeed. When
`plan.outputs.run` is `true`, `contracts` and `probe` must both be `success`;
when it is `false`, both must be `skipped`. Any other combination, a missing
result, or a missing or inconsistent `run` value fails the gate. The
[workflow regression test](../../tools/harness-compat/workflow_test.go) runs
the gate's actual shell command against successful and unsuccessful upstream
results.

The repository's active `protected-main` ruleset requires this check from
GitHub Actions, alongside the other required checks. A workflow's name alone
does not make it a merge requirement: forks and newly configured repositories
must select the stable check in their own branch protection or ruleset. Do not
require the individual matrix jobs by name. The aggregate gate picks up a new
harness automatically.

Native regressions and image or setup failures block merging. Documented
config-only coverage and expected compiler omissions count according to the
tested contract; the gate does not upgrade them to native verification. The
release job in `ci.yml` does not depend on this check, and image publication
follows its own workflow.

If you turn on this requirement before the workflow reaches the default
branch, other open PRs need the workflow update before they can satisfy the
check. Do not bypass protection or weaken assertions to work around a missing
or failing result.

## Isolation and trust

Native executables and containers run in GitHub Actions, not during ordinary
local verification. The test container uses `--network none`, drops all
capabilities, sets `no-new-privileges`, and receives no vendor credentials.
Each subprocess gets an allowlisted environment with isolated HOME, XDG, and
Codex roots and sanitized Git settings.

Trust belongs to the execution workspace, not to a resource's installation
scope. Gemini, for example, suppresses user-level MCP connections in an
untrusted workspace. Native CI sets `HARNESS_COMPAT_TRUST_FIXTURE=1`, and the
[trust helper](../../e2e/harness_compatibility_trust_test.go) confines its
writes to a real disposable repository and home pair. Claude and Cursor
approval is limited to the two fixture MCP names; no broad approval switch is
used.

These trust changes are test setup. Agent Marketplace synchronization never
grants workspace trust or server approval, and hook discovery is not hook
execution.

## Images and versions

The [daily image workflow](../../.github/workflows/harness-images.yml) builds
Linux/amd64 images on a daily schedule and on manual dispatch from the default
branch. It picks up the newest stable releases immediately, independently of
the repository's normal dependency-age policy. Only trusted default-branch
jobs publish to GHCR, and only after an offline version smoke check.
Version and run tags are immutable by convention. The moving `ci` tag
advances only after that smoke check, regardless of compiler compatibility
results, so a newly built, runnable CLI image can surface upstream
regressions.

The [PR compatibility workflow](../../.github/workflows/harness-compatibility.yml)
checks the target, recipe, and Go-version labels before it reuses a published
image. It runs the immutable image ID, or builds an unpublished candidate when
the image is missing or the recipe changed. Reports show the image source,
actual version, identity, age, and a stale-image warning.

Release sources differ by harness. npm packages use stable package metadata.
Devin supplies a bundle manifest with a SHA256 checksum. Cursor's public
installer identifies a calendar-versioned bundle; the resolver parses the
installer text and never executes it, and it records the downloaded bundle's
digest without presenting it as a publisher-provided checksum. See
[release resolution](../../tools/harness-compat/resolve.go),
[installation](../../tools/harness-compat/install.sh), and
[image construction](../../tools/harness-compat/build-image.sh).

## Adding or extending a harness

1. Implement and register the target's compiler, ingester, layout, and
   diagnostics independently of the validation driver.
2. Audit every resource kind against current CLI help, official protocol
   documentation, and source where available. A session list is not an
   agent-definition list, and SDK-provided callbacks are not an installed-hook
   inventory.
3. Register the executable and its release source in
   [the image capability map](../../tools/harness-compat/main.go), or record
   there why no native executable can be tested.
4. Add independently authored expected fixture paths, including split roots
   and scope overrides.
5. Add a probe for every usable surface in `compatibilityResourceProbes`.
   Give each remaining kind an explicit limitation in
   `compatibilityNativeGap`, and update the target guide with the evidence.
6. Add negative controls, assertions on actual entries and fields,
   malformed-response checks, and lifecycle and credential boundaries. A
   process that exits zero does not certify a resource.
7. Keep trust and approval fixture-local, and keep discovery, connection, and
   execution distinct. Do not inject substitute resource definitions through
   protocol options.
8. Run the local compiler and materializer regressions and the commands
   below, then validate the actual executable in CI. Record new native
   findings instead of suppressing them to get green checks.
9. Update this directory, and audit changed fixtures and reports for private
   infrastructure identifiers and credentials before pushing.

## Local verification

```bash
make harness-compat-check
make harness-compat-config
```

`make harness-compat-check` runs the `tools/harness-compat` tests, the E2E
`TestCompatibility*` driver-boundary tests with `HARNESS_COMPAT_TARGET`
empty, and shellcheck on the image scripts, all without vendor executables.
`make harness-compat-config` runs every target, scope, and mode combination of
`TestHarnessCompatibility` in explicit configuration-only mode
(`HARNESS_COMPAT_NATIVE_MODE=unavailable`).

`make validate-e2e` already runs the whole E2E module with the same
configuration-only settings plus the shellcheck step, so after it passes you
do not need to run `harness-compat-config` separately. It does not run the
`tools/harness-compat` package tests. None of these targets replaces native
CI.

With `HARNESS_COMPAT_TARGET` unset, ordinary E2E runs never invoke vendor
CLIs. `HARNESS_COMPAT_REPORT_DIR` selects where the driver keeps structured
evidence. Devin also has a separate local runtime probe suite, `make
devin-probe`; see the [Devin guide](devin.md).

## Target guides

- [Claude](claude.md)
- [Factory](factory.md)
- [Cursor](cursor.md)
- [Windsurf](windsurf.md)
- [Gemini](gemini.md)
- [Devin](devin.md)
- [Codex](codex.md)

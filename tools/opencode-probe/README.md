# OpenCode native compatibility gate

This offline gate executes the supported command-hook adapter against OpenCode
**1.18.34** on Linux arm64. It uses a synthetic loopback model endpoint and
synthetic handlers. No model account or host credentials are needed.

## Reproduce

Download `opencode-linux-arm64.tar.gz` from the upstream
[v1.18.34 release](https://github.com/anomalyco/opencode/releases/tag/v1.18.34).
Verify the archive SHA-256 before extracting:

```text
bbdb3f00c2c51e42e315525233151309724226a8776da8e9145e3b0fa3d5310f
```

The launcher verifies the extracted executable SHA-256:

```text
415487fcd9b3f06addd6877435783f3d5a236c029066b0dd6597d64980b15351
```

Use a Go toolchain satisfying `go.mod`, a cached Linux arm64 Ubuntu 24.04 image,
Docker, and the extracted executable:

```sh
OPENCODE_PROBE_BINARY=/absolute/path/to/opencode make opencode-probe
```

The launcher cross-compiles the test binary and runs it with the pinned native
binary in a container without external networking. HOME, XDG roots,
configuration, dependencies, working directories, and environment are isolated.
Synthetic dependency manifests satisfy the native loader without installation.
Go setup uses the local toolchain with module downloads disabled. The launcher
bounds execution and cleans up its own container and process group.

Eight required tests are witnessed through `tools/witness-check`; missing,
skipped, or failing witnesses fail the gate. Normal root tests require no
OpenCode executable or container runtime.

## Executed contract

| Native callback state | Callback reached | Protected file written |
|---|---|---|
| Active allow | Yes | Yes |
| Active block | Yes | No |
| Absent | No | Yes |
| Disabled | No | Yes |
| Initialization throws | No | Yes |

Additional executed cases cover compiled and actually installed declarations,
a nonmatching ordinary-tool control, handler failures, timeouts, output size
boundaries, declaration-count boundaries, both materialization modes, sibling
hook removal, native permission denial, and a coexisting handwritten plugin.
Native discovery checks cover installed skills, commands, agents and MCP
references in both modes. TodoWrite matching and every handler at the
63/64 invocation boundaries are also executed.
Native startup may format configuration; the gate verifies that existing
provider, permission, and authentication values remain unchanged.

Absent, disabled, or failed plugins are inactive under native semantics.
They are never counted as enforced. MCP and nested subagent tool coverage is
not advertised. Forced termination of the native parent is outside the command
adapter's cleanup guarantee. See the [compatibility note](../../docs/harness-validation/opencode.md)
for protocol details and the distinction between executed cases and code-enforced
bounds. Raw process logs remain private.

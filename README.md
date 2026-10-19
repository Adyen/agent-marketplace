# agent-marketplace

`agent-marketplace` is a command-line tool that installs shared AI agent
resources into the AI coding tools you use. A resource is a skill, command,
subagent, hook, or MCP server. The tool reads resources from registries (Git
repositories or local folders) and from hosted Marketplaces, then writes each
one in the native layout of every harness you target: Factory Droid, Claude
Code, Cursor, Windsurf, Gemini CLI, Devin CLI, Codex, and OpenCode.

It ships as a single Go binary for macOS and Linux with no runtime
dependencies.

**Documentation: [agent-marketplace.io/docs](https://agent-marketplace.io/docs/)**

## Install

Run the installer to detect your platform, download the latest release, verify
its `SHA256SUMS`, and install to `~/.local/bin` without `sudo`:

```bash
curl -fsSL https://agent-marketplace.io/install.sh | bash
```

Set `AGENT_MARKETPLACE_VERSION` to pin a release or
`AGENT_MARKETPLACE_INSTALL_DIR` to choose another directory. For example,
with `vX.Y.Z` replaced by a release tag:

```bash
curl -fsSL https://agent-marketplace.io/install.sh | AGENT_MARKETPLACE_VERSION=vX.Y.Z AGENT_MARKETPLACE_INSTALL_DIR="$HOME/bin" bash
```

To inspect it first, download the script with
`curl -fsSL https://agent-marketplace.io/install.sh -o install.sh`, read it,
then run `bash install.sh`. Releases up to v5.0.0 do not publish `SHA256SUMS`,
so the installer refuses them; use the options below for those releases.

### More install options

Download the binary for your platform from the
[latest release](https://github.com/Adyen/agent-marketplace/releases/latest),
make it executable, and put it on your `PATH`:

```bash
chmod +x agent-marketplace-*-darwin-arm64
mv agent-marketplace-*-darwin-arm64 /usr/local/bin/agent-marketplace
agent-marketplace --version
```

The [install guide](https://agent-marketplace.io/docs/install) covers the other
platforms, installing without administrator rights, `go install`, and building
from source.

## Quick start

```bash
agent-marketplace init                     # choose your target harnesses
agent-marketplace install <name> --from <git-url> --ref main
agent-marketplace sync                     # write resources into your harness folders
agent-marketplace status                   # see what is installed and from where
```

Run `agent-marketplace` with no arguments to browse and toggle resources in the
interactive terminal UI. The configuration lives in `.agents/config.yaml`;
commit it with `.agents/lock.yaml` so every clone gets the same files, and use
`agent-marketplace sync --check` in CI to catch drift.

Continue with the [quick start guide](https://agent-marketplace.io/docs/quick-start),
or jump to the [CLI reference](https://agent-marketplace.io/docs/reference/cli),
[configuration reference](https://agent-marketplace.io/docs/reference/configuration),
and [harnesses and output paths](https://agent-marketplace.io/docs/reference/harnesses-and-output-paths).

## Hosted Marketplace

`agent-marketplace serve` runs a Marketplace server that indexes registries so
teams can search them in one place and install verified, exact revisions. Start
with [run a local Marketplace](https://agent-marketplace.io/docs/tutorials/run-a-local-marketplace)
or [deploy a production Marketplace](https://agent-marketplace.io/docs/how-to/operator/deploy-a-production-marketplace).

## Development

Read [`CODING_STANDARDS.md`](./CODING_STANDARDS.md) before changing code, and
[`AGENTS.md`](./AGENTS.md) for the build, test, and validation workflow.

```bash
make build      # build ./agent-marketplace
make test       # short-mode tests for packages changed vs origin/main
make validate   # selected tests, lint, and build
make e2e        # end-to-end suite (inside Docker)
```

The documentation site sources live in [`site/`](./site).

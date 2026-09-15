# Install

`agent-marketplace` is a single static binary with no runtime dependencies. Install it with one of the methods below, then confirm it runs.

## With Go

If you have Go 1.26 or newer:

```bash
go install github.com/Adyen/agent-marketplace/cmd/agent-marketplace@latest
```

The binary is placed in `$(go env GOPATH)/bin`, usually `~/go/bin`. Make sure that directory is on your `PATH`.

## From GitHub Releases

Download the binary for your platform from the [latest release](https://github.com/Adyen/agent-marketplace/releases/latest). Assets are named `agent-marketplace-<version>-<os>-<arch>`, for example:

```text
agent-marketplace-v3.1.1-darwin-arm64
agent-marketplace-v3.1.1-darwin-amd64
agent-marketplace-v3.1.1-linux-amd64
```

Rename the file to `agent-marketplace`, make it executable, and move it onto your `PATH`:

```bash
chmod +x agent-marketplace-v3.1.1-darwin-arm64
mv agent-marketplace-v3.1.1-darwin-arm64 /usr/local/bin/agent-marketplace
```

## From source

Clone the repository and build with `make`. This also requires Go 1.26 or newer:

```bash
git clone https://github.com/Adyen/agent-marketplace.git
cd agent-marketplace
make build
```

The binary is written to `./agent-marketplace` in the repository root.

## Verify

```bash
agent-marketplace --version
```

A released build prints its version, for example `v3.1.1`. A build from source prints `dev`.

## Supported harnesses

`agent-marketplace` supports seven coding harnesses. The [harnesses and output paths reference](./reference/harnesses-and-output-paths.md) lists their identifiers and native output paths.

## Next

Continue with the [quick start](./quick-start.md).

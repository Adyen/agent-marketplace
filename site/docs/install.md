# More install options

The [quick start](./quick-start.md) installs `agent-marketplace` with the install script. This page covers the script's options and the other ways to install it.

`agent-marketplace` is one program file with no other runtime dependencies. It runs on macOS and Linux, on Intel (`amd64`) and ARM (`arm64`) processors. There is no Windows build.

Pick a method below, then [check that it works](#verify). You also need [Git](./glossary.md#git) to install resources from Git registries. Run `git --version` to check.

::: tip Upgrading an existing installation
`agent-marketplace` refuses to load a root configuration file that doesn't declare `apiVersion: agent-marketplace/v4` (included fragments may omit it): commands exit with an error and the TUI shows it, naming the file. [Convert configuration before upgrading](./how-to/migrate-configuration.md) explains how to fix it.
:::

## Install script

Run the installer to detect your platform, download the latest release, verify its `SHA256SUMS`, and install to `~/.local/bin` without `sudo`:

```bash
curl -fsSL https://agent-marketplace.io/install.sh | bash
```

Set `AGENT_MARKETPLACE_VERSION` to pin a release or `AGENT_MARKETPLACE_INSTALL_DIR` to choose another directory. For example, with `vX.Y.Z` replaced by a release tag:

```bash
curl -fsSL https://agent-marketplace.io/install.sh | AGENT_MARKETPLACE_VERSION=vX.Y.Z AGENT_MARKETPLACE_INSTALL_DIR="$HOME/bin" bash
```

To inspect the script first, download it with `curl -fsSL https://agent-marketplace.io/install.sh -o install.sh`, read it, then run `bash install.sh`. Releases up to v5.0.0 do not publish `SHA256SUMS`, so the installer refuses them; use the options below for those releases.

## From GitHub Releases

1. Find out which build your computer needs:

   ```bash
   uname -sm
   ```

   | Output | Platform in the file name |
   |---|---|
   | `Darwin arm64` | `darwin-arm64` (Mac with Apple silicon) |
   | `Darwin x86_64` | `darwin-amd64` (Mac with an Intel processor) |
   | `Linux x86_64` | `linux-amd64` |
   | `Linux aarch64` | `linux-arm64` |

2. Download that file from the [latest release](https://github.com/Adyen/agent-marketplace/releases/latest). Files are named `agent-marketplace-<version>-<os>-<arch>`. For v5.0.0 they are:

   ```text
   agent-marketplace-v5.0.0-darwin-arm64
   agent-marketplace-v5.0.0-darwin-amd64
   agent-marketplace-v5.0.0-linux-amd64
   agent-marketplace-v5.0.0-linux-arm64
   ```

3. Put the file on your `PATH`, the list of folders your terminal searches for commands. Pick one option below. In both, replace the file name with yours and run the commands in the folder that holds the download (usually `cd ~/Downloads`).

### Option A: install for every user in `/usr/local/bin`

`/usr/local/bin` is on the `PATH` on macOS and most Linux systems. Writing there needs administrator rights, so the last two commands start with `sudo`, which asks for your password:

```bash
chmod +x agent-marketplace-v5.0.0-darwin-arm64
sudo mkdir -p /usr/local/bin
sudo mv agent-marketplace-v5.0.0-darwin-arm64 /usr/local/bin/agent-marketplace
```

`chmod +x` marks the file as a program. `mv` moves it and renames it to `agent-marketplace` at the same time.

### Option B: install for your user only in `~/.local/bin`

This option doesn't need `sudo`:

```bash
chmod +x agent-marketplace-v5.0.0-darwin-arm64
mkdir -p ~/.local/bin
mv agent-marketplace-v5.0.0-darwin-arm64 ~/.local/bin/agent-marketplace
```

Many Linux systems already have `~/.local/bin` on the `PATH`. If yours doesn't (macOS doesn't by default), add it to your shell's startup file and open a new terminal window:

```bash
echo 'export PATH="$HOME/.local/bin:$PATH"' >> ~/.zshrc
```

That line is for zsh, the default shell on macOS. If you use bash, write to `~/.bashrc` instead.

### If macOS refuses to open the file

If macOS says it cannot open `agent-marketplace` because it cannot verify the developer, the download is quarantined. Remove the flag:

```bash
xattr -d com.apple.quarantine /usr/local/bin/agent-marketplace
```

Use `~/.local/bin/agent-marketplace` instead if you chose option B.

## With Go

If you have Go 1.27.3 or newer, you can build and install the newest code from the main branch in one step:

```bash
go install github.com/Adyen/agent-marketplace/cmd/agent-marketplace@latest
```

This builds the latest commit on `main`, which may be newer than the latest release. Go can't select `v4.x.x` release tags for this module, so `@latest` never means a release. To install a release, build it from source (next section).

Go puts the program in `$(go env GOBIN)` if set, otherwise `$(go env GOPATH)/bin` (usually `~/go/bin`). Add that folder to your `PATH` if it isn't there, as in option B. If Go reports a `404` or an authentication error during download, run `export GOPRIVATE="${GOPRIVATE:+$GOPRIVATE,}github.com/Adyen/agent-marketplace"` and try again. Go then downloads directly from GitHub with your Git credentials.

## From source

Clone the release tag and build it with `make`. This also requires Go 1.27.3 or newer:

```bash
git clone --branch v5.0.0 https://github.com/Adyen/agent-marketplace.git
cd agent-marketplace
make build
```

Leave out `--branch v5.0.0` to build the main branch instead. `make build` writes the program to `./agent-marketplace` in the repository folder. Copy it onto your `PATH` as described above. `make release` builds all four release platforms into `dist/`.

## Upgrade notices

About once a day the program checks GitHub for a newer release. When one exists, a command run in a terminal prints one `info:` line after it finishes, and the TUI shows the same notice once. The check never changes a command's output or exit status, and failed checks stay silent. Builds from `go install ...@latest` are compared by commit date, so the notice says a release came out after your build.

The check is skipped in CI (when `CI` is set), when standard error is not a terminal, and for `serve`, shell completion, and `--version`. To turn it off, set `AGENT_MARKETPLACE_NO_UPDATE_CHECK=1`, or add `update_check: {enabled: false}` to `~/.agents/config.yaml` (the file must declare `apiVersion: agent-marketplace/v4`). Development builds never check.

## Verify

Open a new terminal window and run:

```bash
agent-marketplace --version
```

The output depends on how you installed it:

| Installed with | Prints, for example |
|---|---|
| The install script | the release tag it installed |
| A release download | `v5.0.0` |
| `go install ...@latest` | a Go pseudo-version such as `v0.0.0-20260927180313-baeda4e602ab` |
| `make build` | the output of `git describe`: `v5.0.0` on the release tag, or something like `v5.0.0-3-gbaeda4e` on a later commit |
| A plain `go build` without version flags | a Go pseudo-version such as `v0.0.0-2026...`, or `dev` when Go recorded no version |

If the terminal answers `command not found`, the folder that holds the program isn't on your `PATH`. Run `echo $PATH` to see the list, and check that you opened a new terminal window after changing it. If it answers `permission denied`, the file isn't marked as a program yet: run `chmod +x` on it.

Teams that commit a lockfile need v4.0.9 or newer on every machine and CI runner that reads it. Earlier releases, including v4.0.8, reject the lockfile format that v4.0.9 writes.

`agent-marketplace` supports seven coding [harnesses](./glossary.md#harness). The [harnesses and output paths reference](./reference/harnesses-and-output-paths.md) lists them and where each one's files go.

## Next

Continue with the [quick start](./quick-start.md).

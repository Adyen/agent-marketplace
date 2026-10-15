# Install

`agent-marketplace` is a single program file with no other runtime dependencies. Builds exist for macOS and Linux on Intel (`amd64`) and ARM (`arm64`) processors. There is no Windows build.

Pick one of the methods below, then [check that it works](#verify). Most people should download a release. If you rarely use a terminal, read [Terminal basics](./terminal-basics.md) first.

You also need [Git](./glossary.md#git) to install resources from Git registries. Run `git --version` to check; [Terminal basics](./terminal-basics.md#install-git) explains how to install it.

::: warning Upgrading an existing installation
Before replacing your binary with the v4-only CLI, [convert every writable root and its include tree](./how-to/migrate-configuration.md) using an older released migrator. Existing organization, user, repository, and personal-local roots all require exact `apiVersion: agent-marketplace/v4`; organization configuration must be converted and deployed externally by its administrator. Conversion can refuse includes, unrepresentable legacy input, or missing verified Marketplace evidence. Do not merely change the version string. The new CLI has no `migrate` command or automatic startup/TUI conversion.
:::

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

2. Download that file from the [latest release](https://github.com/Adyen/agent-marketplace/releases/latest). Files are named `agent-marketplace-<version>-<os>-<arch>`. For v4.1.4 they are:

   ```text
   agent-marketplace-v4.1.4-darwin-arm64
   agent-marketplace-v4.1.4-darwin-amd64
   agent-marketplace-v4.1.4-linux-amd64
   agent-marketplace-v4.1.4-linux-arm64
   ```

3. Put the file on your `PATH`. The `PATH` is the list of folders your terminal searches when you type a command name; [Terminal basics](./terminal-basics.md#make-a-command-available-everywhere) explains it. Choose one of the two options below. In both, replace the file name with the one you downloaded, and run the commands from the folder that contains it (usually `cd ~/Downloads`).

### Option A: install for every user in `/usr/local/bin`

`/usr/local/bin` is on the `PATH` on macOS and most Linux systems. Writing to it needs administrator rights, so the last two commands start with `sudo`, which asks for your computer password:

```bash
chmod +x agent-marketplace-v4.1.4-darwin-arm64
sudo mkdir -p /usr/local/bin
sudo mv agent-marketplace-v4.1.4-darwin-arm64 /usr/local/bin/agent-marketplace
```

`chmod +x` marks the file as a program. `mv` moves it and renames it to `agent-marketplace` at the same time.

### Option B: install for your user only in `~/.local/bin`

This option doesn't need `sudo`:

```bash
chmod +x agent-marketplace-v4.1.4-darwin-arm64
mkdir -p ~/.local/bin
mv agent-marketplace-v4.1.4-darwin-arm64 ~/.local/bin/agent-marketplace
```

Many Linux systems already have `~/.local/bin` on the `PATH`. If yours doesn't (macOS doesn't by default), add it to your shell's startup file and open a new terminal window:

```bash
echo 'export PATH="$HOME/.local/bin:$PATH"' >> ~/.zshrc
```

That line is for zsh, the default shell on macOS. If you use bash, write to `~/.bashrc` instead.

### If macOS refuses to open the file

If macOS says it cannot open `agent-marketplace` because it cannot verify the developer, it has quarantined the downloaded file. You can remove that flag from the installed file:

```bash
xattr -d com.apple.quarantine /usr/local/bin/agent-marketplace
```

Use `~/.local/bin/agent-marketplace` instead if you chose option B.

## With Go

If you have Go 1.26.9 or newer, you can build and install the newest code from the main branch in one step:

```bash
go install github.com/Adyen/agent-marketplace/cmd/agent-marketplace@latest
```

This builds the latest commit on the main branch, which may be newer than the latest release. Go can't select the `v4.x.x` release tags for this module, so `@latest` never means a release. To install a release with Go, build it from source as described in the next section.

Go places the program in `$(go env GOPATH)/bin`, usually `~/go/bin`. Add that folder to your `PATH` if it isn't there already, the same way as in option B above. If Go reports a `404` or an authentication error while downloading, run `export GOPRIVATE=github.com/Adyen/agent-marketplace` and try again, so Go downloads directly from GitHub with your Git credentials.

## From source

Clone the release tag and build it with `make`. This also requires Go 1.26.9 or newer:

```bash
git clone --branch v4.1.4 https://github.com/Adyen/agent-marketplace.git
cd agent-marketplace
make build
```

Leave out `--branch v4.1.4` to build the main branch instead. `make build` writes the program to `./agent-marketplace` in the repository folder. Copy it onto your `PATH` as described above. `make release` builds all four release platforms into `dist/`.

## Verify

Open a new terminal window and run:

```bash
agent-marketplace --version
```

The output depends on how you installed it:

| Installed with | Prints, for example |
|---|---|
| A release download | `v4.1.4` |
| `go install ...@latest` | a Go pseudo-version such as `v0.0.0-20260927180313-baeda4e602ab` |
| `make build` | the output of `git describe`: `v4.1.4` on the release tag, or something like `v4.1.4-3-gbaeda4e` on a later commit |
| A plain `go build` without version flags | a Go pseudo-version such as `v0.0.0-2026...`, or `dev` when Go recorded no version |

If the terminal answers `command not found`, the folder that holds the program isn't on your `PATH`. Run `echo $PATH` to see the list, and check that you opened a new terminal window after changing it. If it answers `permission denied`, the file isn't marked as a program yet: run `chmod +x` on it.

Teams that commit a lockfile need v4.0.9 or newer on every machine and CI runner that reads it. Earlier releases, including v4.0.8, reject the lockfile format that v4.0.9 writes.

`agent-marketplace` supports seven coding [harnesses](./glossary.md#harness). The [harnesses and output paths reference](./reference/harnesses-and-output-paths.md) lists them and where each one's files go.

## Next

Continue with the [quick start](./quick-start.md).

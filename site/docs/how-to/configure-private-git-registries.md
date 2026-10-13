# How to configure private Git registries

Let `agent-marketplace` read a private Git [registry](../glossary.md#registry) using the Git credentials you already have, without putting credentials in the `agent-marketplace` configuration or lock.

## Prerequisites

- `agent-marketplace` on your `PATH`.
- A private registry you can clone over SSH or HTTPS.
- Working Git credentials on this machine: an SSH key, or a Git credential helper for HTTPS.

## 1. Prepare your credentials

`agent-marketplace` runs your `git` program with a restricted setup: Git never prompts for a username or password, and only a limited set of your Git environment and configuration is applied. The full list of passed-through variables and copied credential settings is in the [environment variables reference](../reference/environment-variables.md). Prepare one transport below; SSH with the standard SSH agent is the most portable option.

- Git never prompts for a username or password. Credentials must come from an SSH agent, an SSH key, or a credential helper.
- Your personal and system Git configuration files are not loaded as a whole. For HTTPS registries, `agent-marketplace` copies only the credential, TLS and redirect settings from them: `credential.helper`, `credential.username`, `credential.useHttpPath`, `http.extraHeader`, the `http.ssl*` certificate settings, and `http.followRedirects`. Other settings, such as `url.<base>.insteadOf` rewrites or `core.sshCommand`, are not applied.
- Redirects follow Git's rules. By default (`http.followRedirects=initial`), Git follows a redirect of the first request and sends the rest of the fetch, including your credentials, to the redirected URL, exactly as `git clone` does. To refuse redirects, set `http.followRedirects=false` in your Git configuration, for all hosts or for one URL (`[http "https://git.example.com/"]`). Redirect failures are reported as `git.auth-transport-failed`.
- Most `GIT_*` environment variables are dropped. `GIT_SSH`, `GIT_SSH_COMMAND`, `GIT_ASKPASS`, `GIT_SSL_CERT`, `GIT_SSL_KEY`, `GIT_SSL_CAINFO`, `GIT_SSL_CAPATH`, `GIT_HTTP_PROXY`, `GIT_HTTPS_PROXY`, and `GIT_NO_PROXY` pass through. `GIT_SSL_NO_VERIFY` is removed. Your SSH agent (`SSH_AUTH_SOCK`) and `~/.ssh/config` work as usual.

### SSH key authentication

Add your key to the agent, replacing the file name with your own key:

```bash
ssh-add ~/.ssh/id_agent_marketplace
```

`ssh-add` prints `Identity added: <path>` on success.

Check that Git can read the repository. This command only reads; it changes no configuration:

```bash
git ls-remote git@github.com:example-org/private-skills.git
```

It prints one line per ref, for example `a1b2c3d4...	HEAD`. If it fails, fix the SSH key, agent, or `~/.ssh/known_hosts` entry before configuring the source.

### HTTPS with a credential helper

If you normally sign in to the host through a credential manager or a personal access token stored in a credential helper, check that `git ls-remote` works:

```bash
git ls-remote https://github.com/example-org/private-skills.git
```

It prints the same ref listing as the SSH check. If it prompts or fails, fix the credential helper first. `agent-marketplace` copies credential and TLS settings only from trusted Git configuration files and their ordinary `[include]` chains; conditional `[includeIf]` sections and repository-local authentication settings are never used. The trust rules and include limits are documented in the [environment variables reference](../reference/environment-variables.md).

Do not run `git config credential.helper store` or change other global Git settings just for `agent-marketplace`.

## 2. Declare the source

Configure the registry with the same URL form you just checked:

```yaml
apiVersion: agent-marketplace/v4
sources:
  private-skills:
    git: git@github.com:example-org/private-skills.git
    ref: main
resources:
  private-skills:
    all: true
```

For HTTPS, replace the `git:` value with `https://github.com/example-org/private-skills.git`.

`agent-marketplace` copies credential settings only from regular, singly linked Git configuration files that group and other users cannot write to, and that are owned by you, `root`, or the account that owns the `git` executable it runs. Symlinks to such files are supported.

The last case covers Git installed under a dedicated account, such as a Homebrew installation with a separate install user. Git's system configuration (`$(brew --prefix)/etc/gitconfig`) then belongs to that account. `agent-marketplace` finds `git` on your `PATH`, follows symlinks to the real executable, and trusts its owner only if it was found through an absolute `PATH` entry and no group or other user can write to it. That account can already change the `git` program itself, so trusting its configuration grants it nothing new.

## 3. Sync and verify

Includes expand in declaration order, preserving credential-helper resets and URL-specific settings. The reader allows ten include levels, at most 128 expanded file visits, 4 MiB per file, and 16 MiB of expanded content. Cycles and malformed files are rejected before the fetch runs. Only credential, TLS and redirect settings are copied; other settings in included files are not applied.

```bash
agent-marketplace sync
```

Sync fetches through your Git credentials and prints the usual `Installed <n>  Removed <n>  Malformed <n>  Errors <n>` summary with one `→` line per output root. Check the locked commit and the selected resources:

```bash
agent-marketplace status
```

`EXACT REGISTRY LOCKS` lists the commit the private registry resolved to, and `SYNCED RESOURCES` lists what it provides.

## Check that the lock holds no credentials

The lock stores each registry URL in a normalized form. It removes user names from HTTPS URLs, drops a trailing `.git`, and writes SSH addresses such as `git@github.com:org/repo.git` as `git+scp://git@github.com/org/repo`. It refuses to lock a URL that contains a password or a query string, and reports `lock.git-url-invalid`.

To see what the lock records:

```bash
grep -E '^\s*url:' .agents/lock.yaml
```

Each line should show only a host and a repository path, such as `https://github.com/example-org/private-skills` or `git+scp://git@github.com/example-org/private-skills`.

## Boundaries

- Never put credentials in a registry URL, a query parameter, or `~/.agents/config.yaml`. Even though the lock does not store them, they stay readable in the configuration file.
- The lock contains exact commits, not credentials.
- Your credential manager is responsible for how long credentials last and for rotating them.

## Troubleshooting

- `git.auth-transport-failed: Git SSH connection failed. Verify your SSH agent and repository access`: check that `ssh-add -l` lists your key, that the key has read access to the repository, and that the host is in `~/.ssh/known_hosts`.
- `git.auth-transport-failed: transport diagnostics withheld to protect authentication data`: an HTTPS fetch failed. `agent-marketplace` hides Git's message because it can contain credentials. Run the `git ls-remote` command above to see the real error, then check that your credential helper is current and has read access.
- `git.auth-config-untrusted`: check the system/global files and their ordinary include chains for conditional includes, unsafe ownership or write permissions, non-regular or hard-linked files, malformed configuration, cycles, or the include limits listed in the [environment variables reference](../reference/environment-variables.md). Repository-local authentication settings are also refused. Do not remove company-managed includes or disable TLS verification to work around a trust failure.
- The registry works in your shell but not in `agent-marketplace`: your setup may depend on a setting that is not applied, such as a global `url.<base>.insteadOf` rewrite or `core.sshCommand`. Put the real URL in the configuration, or set `GIT_SSH_COMMAND` in the environment instead of `core.sshCommand`.

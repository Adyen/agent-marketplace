# How to configure private Git registries

Let `agent-marketplace` read a private Git [registry](../glossary.md#registry) using the Git credentials you already have, without putting credentials in the `agent-marketplace` configuration or lock.

## Prerequisites

- `agent-marketplace` on your `PATH`.
- A private registry you can clone over SSH or HTTPS.
- Working Git credentials on this machine: an SSH key, or a Git credential helper for HTTPS.

## How `agent-marketplace` runs Git

`agent-marketplace` runs your `git` program with a restricted setup, so a command that works in your shell does not always work unchanged:

- Git never prompts for a username or password. Credentials must come from an SSH agent, an SSH key, or a credential helper.
- Your personal and system Git configuration files are not loaded as a whole. For HTTPS registries, `agent-marketplace` copies only the credential and TLS settings from them: `credential.helper`, `credential.username`, `credential.useHttpPath`, `http.extraHeader`, and the `http.ssl*` certificate settings. Other settings, such as `url.<base>.insteadOf` rewrites or `core.sshCommand`, are not applied.
- Most `GIT_*` environment variables are dropped. `GIT_SSH`, `GIT_SSH_COMMAND`, `GIT_ASKPASS`, `GIT_SSL_CERT`, `GIT_SSL_KEY`, `GIT_SSL_CAINFO`, `GIT_SSL_CAPATH`, `GIT_HTTP_PROXY`, `GIT_HTTPS_PROXY`, and `GIT_NO_PROXY` pass through. `GIT_SSL_NO_VERIFY` is removed. Your SSH agent (`SSH_AUTH_SOCK`) and `~/.ssh/config` work as usual.

## Use SSH key authentication

SSH with the standard SSH agent is the most portable option.

1. Add your key to the agent. Replace the file name with your own key:

   ```bash
   ssh-add ~/.ssh/id_agent_marketplace
   ```

2. Check that Git can read the repository. This command only reads; it changes no configuration:

   ```bash
   git ls-remote git@github.com:example-org/private-skills.git
   ```

3. Configure the registry in `agent-marketplace` with the same SSH URL:

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

## Use HTTPS with a credential helper

If you normally sign in to the host through a credential manager or a personal access token stored in a credential helper, check that `git ls-remote` works:

```bash
git ls-remote https://github.com/example-org/private-skills.git
```

Then configure the same URL in `agent-marketplace`:

```yaml
apiVersion: agent-marketplace/v4
sources:
  private-skills:
    git: https://github.com/example-org/private-skills.git
    ref: main
resources:
  private-skills:
    all: true
```

`agent-marketplace` only copies a credential setting from a Git configuration file that you or `root` own and that other users cannot write to. It refuses to use HTTPS credentials at all when those files contain `[include]` or `[includeIf]` sections.

Do not run `git config credential.helper store` or change other global Git settings just for `agent-marketplace`.

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
- `git.auth-config-untrusted`: one of your Git configuration files uses `[include]` or `[includeIf]`, can be written by other users, or is owned by someone else. Move the credential settings into a plain `~/.gitconfig` that only you can write.
- The registry works in your shell but not in `agent-marketplace`: your setup may depend on a setting that is not applied, such as a global `url.<base>.insteadOf` rewrite or `core.sshCommand`. Put the real URL in the configuration, or set `GIT_SSH_COMMAND` in the environment instead of `core.sshCommand`.
